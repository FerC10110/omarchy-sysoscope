package apps

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

func dispatches(env source.Env) []string { return env.Hypr.(*source.FakeHypr).Dispatches() }

func TestSpecs(t *testing.T) {
	if len(Specs) != 20 {
		t.Fatalf("%d actions", len(Specs))
	}
	keys := map[string]bool{}
	for _, s := range Specs {
		if keys[s.Key] || s.Label("en") == "" || s.Label("es") == s.Label("en") {
			t.Errorf("%s: repeated key or missing label", s.ID)
		}
		keys[s.Key] = true
		if s.Key == "X" || s.Key == "j" || s.Key == "k" || s.Key == "h" || s.Key == "l" || s.Key == " " {
			t.Errorf("%s uses %q, which the panel does not get as text", s.ID, s.Key)
		}
	}
}

func TestActionDispatches(t *testing.T) {
	a1, b1, c1 := `window="address:0xa1"`, `window="address:0xb1"`, `window="address:0xc1"`
	for _, c := range []struct {
		id, target, input string
		want              []string
	}{
		{"window.focus", "0xa1", "", []string{`hl.dsp.focus({` + a1 + `})`}},
		{"window.float", "0xa1", "", []string{`hl.dsp.window.float({action="toggle",` + a1 + `})`}},
		{"window.pin", "0xb1", "", []string{`hl.dsp.window.pin({` + b1 + `})`}},
		{"window.fullscreen", "0xa1", "", []string{`hl.dsp.window.fullscreen({mode="fullscreen",` + a1 + `})`}},
		{"window.maximize", "0xa1", "", []string{`hl.dsp.window.fullscreen({mode="maximized",` + a1 + `})`}},
		{"window.to_workspace", "0xa1", "4", []string{`hl.dsp.window.move({workspace="4",follow=true,` + a1 + `})`}},
		{"window.to_workspace_silent", "0xa1", " 4 ", []string{`hl.dsp.window.move({workspace="4",follow=false,` + a1 + `})`}},
		{"window.special", "0xa1", "", []string{`hl.dsp.window.move({workspace="special:sysoscope",follow=false,` + a1 + `})`}},
		{"window.special", "0xb1", "", []string{`hl.dsp.window.move({workspace="1",follow=true,` + b1 + `})`}},
		{"window.to_monitor", "0xa1", "HDMI-A-1", []string{`hl.dsp.window.move({monitor="HDMI-A-1",` + a1 + `})`}},
		{"window.center", "0xa1", "", []string{`hl.dsp.window.float({action="toggle",` + a1 + `})`, `hl.dsp.window.center({` + a1 + `})`}},
		{"window.center", "0xb1", "", []string{`hl.dsp.window.center({` + b1 + `})`}},
		{"window.resize", "0xa1", "50%", []string{`hl.dsp.window.float({action="toggle",` + a1 + `})`,
			`hl.dsp.window.resize({x=1280,y=720,` + a1 + `})`, `hl.dsp.window.center({` + a1 + `})`}},
		// bring_to_top no acepta ventana: sube la que tiene el foco.
		{"window.raise", "0xb1", "", []string{`hl.dsp.window.alter_zorder({mode="top",` + b1 + `})`}},
		{"window.group", "0xa1", "", []string{`hl.dsp.group.toggle({` + a1 + `})`}},
		{"window.opaque", "0xa1", "", []string{`hl.dsp.window.set_prop({prop="opaque",value="toggle",` + a1 + `})`}},
		{"window.inhibit_idle", "0xa1", "", []string{`hl.dsp.window.set_prop({prop="idle_inhibit",value="always",` + a1 + `})`}},
		{"window.inhibit_idle", "0xc1", "", []string{`hl.dsp.window.set_prop({prop="idle_inhibit",value="none",` + c1 + `})`}},
		{"window.close", "0xa1", "", []string{`hl.dsp.window.close({` + a1 + `})`}},
		// La fila lleva la dirección y el stableId.
		{"window.close", "0xa1:20", "", []string{`hl.dsp.window.close({` + a1 + `})`}},
		{"app.btop", "app:comm:syncthing", "", []string{`hl.dsp.exec_cmd("xdg-terminal-exec btop --filter syncthing")`}},
	} {
		env := tempApps(t)
		if _, err := Run(env, c.id, c.target, c.input); err != nil {
			t.Errorf("%s %s: %v", c.id, c.target, err)
			continue
		}
		if got := dispatches(env); !slices.Equal(got, c.want) {
			t.Errorf("%s %s:\n got %q\nwant %q", c.id, c.target, got, c.want)
		}
	}
}

// Hyprland contesta "ok" a hl.dsp.focus aunque no mueva el foco (por
// ejemplo, con una capa que retiene el teclado): se mira j/activewindow.
func TestFocusNotMoved(t *testing.T) {
	for _, c := range []struct{ id, input, want string }{
		{"window.focus", "", "the window did not get the keyboard focus: something else is holding it"},
		// La ventana se movió: se dice, y que el foco no la siguió.
		{"window.to_workspace", " 4", "~ moved to workspace 4, but the keyboard focus did not follow it"},
	} {
		env := tempApps(t)
		// La ventana activa sigue siendo 0xa1.
		_, err := Run(env, c.id, "0xb1", c.input)
		if err == nil || err.Error() != c.want {
			t.Errorf("%s: err = %v", c.id, err)
		}
	}
	env := tempApps(t)
	os.WriteFile(filepath.Join(env.Hypr.(*source.FakeHypr).Dir, "j-activewindow.txt"), []byte(`{"address":"0xb1"}`), 0o644)
	if out, err := Run(env, "window.focus", "0xb1", ""); err != nil || out != "Go to window: ~" {
		t.Fatalf("focus moved: %q, %v", out, err)
	}
}

func TestActionResultText(t *testing.T) {
	out, err := Run(tempApps(t), "window.float", "0xa1", "")
	if err != nil || out != "Toggle floating: GitHub — Chromium" {
		t.Fatalf("out = %q, %v", out, err)
	}
}

func TestActionErrors(t *testing.T) {
	for _, c := range []struct{ id, target, input, want string }{
		{"window.nope", "0xa1", "", `unknown action "window.nope"`},
		{"window.float", "0xdead", "", "the window no longer exists"},
		// La dirección ahora es de otra ventana (Hyprland la reusó).
		{"window.close", "0xa1:99", "", "the window no longer exists"},
		{"app.kill", "app:nope", "", "the app is no longer running"},
		{"window.float", "app:comm:syncthing", "", "this app has no window"},
		{"window.pin", "0xa1", "", "only a floating window can be pinned"},
		{"window.to_workspace", "0xa1", "abc", "a workspace from 1 to 99"},
		{"window.to_workspace", "0xa1", "100", "a workspace from 1 to 99"},
		{"window.to_monitor", "0xa1", "VGA-9", `unknown monitor "VGA-9"`},
		{"window.resize", "0xa1", "33%", "one of: 50%, 70%, 90%"},
		{"app.kill", "app:comm:Hyprland", "", "Hyprland keeps the session running"},
		{"app.freeze", "app:comm:pipewire", "", "pipewire keeps the session running"},
	} {
		env := tempApps(t)
		_, err := Run(env, c.id, c.target, c.input)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %s %q: err = %v, want %q", c.id, c.target, c.input, err, c.want)
		}
		if d := dispatches(env); len(d) != 0 {
			t.Errorf("%s %s dispatched %q after failing", c.id, c.target, d)
		}
	}
}

func TestActionHyprlandRejects(t *testing.T) {
	env := tempApps(t)
	expr := `hl.dsp.window.center({window="address:0xb1"})`
	dir := env.Hypr.(*source.FakeHypr).Dir
	os.WriteFile(filepath.Join(dir, source.Key("dispatch "+expr)+".txt"), []byte("info: =[C]:-1: No floating window found\n"), 0o644)
	_, err := Run(env, "window.center", "0xb1", "")
	if err == nil || err.Error() != "No floating window found" {
		t.Fatalf("err = %v", err)
	}
}

func TestActionWithoutHyprland(t *testing.T) {
	env := tempApps(t)
	env.Hypr = &source.FakeHypr{Dir: t.TempDir()}
	if _, err := Run(env, "window.float", "0xa1", ""); err == nil || err.Error() != "Hyprland is not running" {
		t.Fatalf("err = %v", err)
	}
}

func TestKillWaitsForTERM(t *testing.T) {
	env := tempApps(t)
	sigs := stubSignals(t, func(pid int, sig syscall.Signal) {
		if sig == syscall.SIGTERM {
			os.RemoveAll(env.FS.Real("/proc/" + itoa(pid)))
		}
	})
	out, err := Run(env, "app.kill", "0xc1", "")
	if err != nil || out != "steam was closed" || !slices.Equal(*sigs, []string{"600 TERM", "601 TERM"}) {
		t.Fatalf("out %q, err %v, signals %v", out, err, *sigs)
	}
}

func TestKillSkipsReusedPID(t *testing.T) {
	env := tempApps(t)
	sigs := stubSignals(t, nil)
	g := groupByID(t, env, "comm:steam")
	os.WriteFile(env.FS.Real("/proc/601/stat"), []byte("601 (other) S 1 0 0 0 -1 0 0 0 0 0 1 1 0 0 20 0 1 0 9999 0 1\n"), 0o644)
	out, _ := kill(env, g)
	if out != "steam was force-closed" || !slices.Equal(*sigs, []string{"600 TERM", "600 KILL"}) {
		t.Fatalf("out %q, signals %v", out, *sigs)
	}
}

func TestKillResumesFrozenApp(t *testing.T) {
	env := tempApps(t)
	sigs := stubSignals(t, nil)
	LoadFreezer(env).freeze(env, groupByID(t, env, "comm:steam"), t0)
	Run(env, "app.kill", "app:comm:steam", "")
	if (*sigs)[2] != "600 CONT" || (*sigs)[4] != "600 TERM" {
		t.Fatalf("signals = %v", *sigs)
	}
}

func TestFreezeAction(t *testing.T) {
	env := tempApps(t)
	stubSignals(t, nil)
	cg := cgFreeze(t, env, groupByID(t, env, "app-org.chromium.Chromium-200.scope"))
	if out, err := Run(env, "app.freeze", "0xa1", ""); err != nil || out != "Chromium is frozen for 5 minutes" || readFile(t, cg) != "1" {
		t.Fatalf("freeze: %q, %v", out, err)
	}
	if out, _ := Run(env, "app.freeze", "0xa2", ""); out != "Chromium was resumed" || readFile(t, cg) != "0" {
		t.Fatalf("resume: %q", out)
	}
}

func TestActionsLog(t *testing.T) {
	env := tempApps(t)
	Run(env, "window.float", "0xa1", "")
	Run(env, "window.to_workspace", "0xdead", "4")
	b, err := os.ReadFile(filepath.Join(env.State, "actions.log"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	// Sin el texto: lleva el título de la ventana (ver actlog).
	if len(lines) != 2 || lines[0] != `2026-09-23T12:00:00Z window.float "0xa1" "" ok` ||
		lines[1] != `2026-09-23T12:00:00Z window.to_workspace "0xdead" "4" error` {
		t.Fatalf("log:\n%s", b)
	}
}

func TestCapture(t *testing.T) {
	env := tempApps(t)
	dir := t.TempDir()
	old := getenv
	getenv = func(k string) string {
		if k == "OMARCHY_SCREENSHOT_DIR" {
			return dir
		}
		return ""
	}
	t.Cleanup(func() { getenv = old })
	path := filepath.Join(dir, "sysoscope-chromium-20260923-120000.png")
	if _, err := Run(env, "window.capture", "0xa1", ""); err == nil || !strings.Contains(err.Error(), "requires grim") {
		t.Fatalf("without grim: %v", err)
	}
	writeCmd(t, env, source.Key("grim", "-g", "0,0 1280x1400", path), "")
	if out, err := Run(env, "window.capture", "0xa1", ""); err != nil || out != "saved to "+path {
		t.Fatalf("capture: %q, %v", out, err)
	}
	if _, err := Run(env, "window.capture", "0xb1", ""); err == nil || !strings.Contains(err.Error(), "not on screen") {
		t.Fatalf("hidden window: %v", err)
	}
}

// btop se filtra por el comm del proceso de la ventana o, sin ventana, por el
// comm más común de la app (no por el del primer proceso).
func TestBtopFilter(t *testing.T) {
	sp := Specs[slices.IndexFunc(Specs, func(s Spec) bool { return s.ID == "app.btop" })]
	pr := func(pid int, comm string) proc { return proc{pkey: pkey{pid: pid}, comm: comm} }
	for _, c := range []struct {
		tg   *target
		want string
	}{
		{&target{app: group{procs: []proc{pr(10, "bash"), pr(11, "bash"), pr(12, "kitty")}}, win: &winRef{pid: 12}}, "kitty"},
		{&target{app: group{procs: []proc{pr(20, "steamwebhelper"), pr(21, "steam"), pr(22, "steam")}}}, "steam"},
		// Pedida por su fila de app ("app:<ID>"), una app con ventana también.
		{&target{app: group{procs: []proc{pr(10, "bash"), pr(11, "bash"), pr(12, "kitty")}},
			hy: &hyprState{windows: map[int][]model.Window{12: {{Address: "0x1"}}}}}, "kitty"},
	} {
		env := tempApps(t)
		if _, err := run(env, sp, c.tg, ""); err != nil {
			t.Fatal(err)
		}
		want := `hl.dsp.exec_cmd("xdg-terminal-exec btop --filter ` + c.want + `")`
		if got := dispatches(env); !slices.Equal(got, []string{want}) {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}
