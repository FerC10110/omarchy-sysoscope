package power

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

const (
	ownerKey = "call org.freedesktop.DBus /org/freedesktop/DBus org.freedesktop.DBus.NameHasOwner org.freedesktop.login1"
	listKey  = "call org.freedesktop.login1 /org/freedesktop/login1 org.freedesktop.login1.Manager.ListInhibitors"
)

// realList son los inhibidores de la máquina de referencia (2026-09-27) más
// un block de la tapa como el del fixture laptop.
const realList = `[[
 ["sleep","Omarchy","Lock screen before suspend","delay",1000,1456],
 ["sleep","UPower","Pause device polling","delay",0,1382],
 ["sleep","NetworkManager","NetworkManager needs to turn off networks","delay",0,1068],
 ["handle-lid-switch","Hyprland","Lid handled by the compositor","block",1000,1180]
]]`

// inhibitEnv: un bus con esas respuestas ("" = sin la clave) y un Hyprland
// que responde j/clients con clients ("" = no hay Hyprland).
func inhibitEnv(t *testing.T, owner, list, clients string) source.Env {
	t.Helper()
	sys := map[string]json.RawMessage{}
	if owner != "" {
		sys[ownerKey] = json.RawMessage(owner)
	}
	if list != "" {
		sys[listKey] = json.RawMessage(list)
	}
	hypr := t.TempDir()
	if clients != "" {
		if err := os.WriteFile(filepath.Join(hypr, "j-clients.txt"), []byte(clients), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return source.Env{FS: source.FS{Root: t.TempDir()}, Bus: &source.FakeBus{System: sys},
		Hypr: &source.FakeHypr{Dir: hypr}, Lang: "en", Home: "/home/user"}
}

func TestInhibitors(t *testing.T) {
	r := &collect.Report{Lang: "en"}
	in := Inhibit(inhibitEnv(t, `[true]`, realList, ""), r)
	if len(r.List) != 0 || len(in.Logind) != 4 || len(in.Windows) != 0 || in.StayAwake {
		t.Fatalf("inhibit = %+v, unavailable %+v", in, r.List)
	}
	want := model.Inhibitor{What: []string{"sleep"}, Who: "Omarchy", Why: "Lock screen before suspend", Mode: "delay", UID: 1000, PID: 1456}
	if !slices.Equal(in.Logind[0].What, want.What) || in.Logind[0].Who != want.Who || in.Logind[0].Why != want.Why ||
		in.Logind[0].Mode != "delay" || in.Logind[0].UID != 1000 || in.Logind[0].PID != 1456 {
		t.Errorf("first = %+v", in.Logind[0])
	}
	if l := in.Logind[3]; l.Mode != "block" || !slices.Equal(l.What, []string{"handle-lid-switch"}) || l.Who != "Hyprland" {
		t.Errorf("lid = %+v", l)
	}
	// Varias cosas en un mismo inhibidor, separadas por ":".
	in = Inhibit(inhibitEnv(t, `[true]`, `[[["sleep:idle:handle-lid-switch","Steam","Downloading","block",1000,42]]]`, ""), r)
	if !slices.Equal(in.Logind[0].What, []string{"sleep", "idle", "handle-lid-switch"}) {
		t.Errorf("what = %q", in.Logind[0].What)
	}
}

// Review Focus 1: tuplas cortas, tipos equivocados, qué vacío y textos con
// caracteres de control; nada de eso rompe la lectura.
func TestInhibitorsMalformed(t *testing.T) {
	list := `[[
	 ["sleep","A","x","block",1000,5],
	 ["idle"],
	 "junk",
	 [1,2,3,4,5,6],
	 ["","B","y","block",0,0],
	 ["sleep","C\u001b[31m","z\u0007","block",-1,1e12]
	]]`
	r := &collect.Report{Lang: "en"}
	in := Inhibit(inhibitEnv(t, `[true]`, list, ""), r)
	if len(r.List) != 0 || len(in.Logind) != 2 {
		t.Fatalf("logind = %+v (%+v)", in.Logind, r.List)
	}
	// M2: `clean` limpia con `text.Clean` (secuencia ANSI/CSI completa, no
	// solo el ESC), no con `wifi.Printable`.
	if c := in.Logind[1]; c.Who != "C" || c.Why != "z" || c.UID != 0 || c.PID != 0 {
		t.Errorf("hostile = %+v", c)
	}
	for _, body := range []string{`[]`, `["x"]`, `[[]]`} {
		if in := Inhibit(inhibitEnv(t, `[true]`, body, ""), r); len(in.Logind) != 0 || in.Logind == nil {
			t.Errorf("%s: %+v", body, in.Logind)
		}
	}
}

func TestU32(t *testing.T) {
	for _, c := range []struct {
		in   any
		want uint32
	}{{uint32(5), 5}, {float64(7), 7}, {-1.0, 0}, {1e12, 0}, {"x", 0}, {nil, 0}, {int32(3), 0}} {
		if got := u32(c.in); got != c.want {
			t.Errorf("u32(%#v) = %d", c.in, got)
		}
	}
}

// Review Focus 2: sin dueño de org.freedesktop.login1 no se llama a
// ListInhibitors, y queda dicho por qué (M6: ausencia si el dueño responde
// false; error si la pregunta misma falla).
func TestInhibitorsWithoutLogind(t *testing.T) {
	for _, c := range []struct {
		owner  string
		reason string
	}{
		{`[false]`, "not_present"}, // logind no tiene dueño: no corre
		{"", "error"},              // la pregunta misma falla: error del bus
	} {
		env := inhibitEnv(t, c.owner, realList, "")
		r := &collect.Report{Lang: "en"}
		in := Inhibit(env, r)
		if n := env.Bus.(*source.FakeBus).Called("ListInhibitors"); n != 0 {
			t.Errorf("owner %q: ListInhibitors called %d times", c.owner, n)
		}
		if len(in.Logind) != 0 || len(r.List) != 1 || r.List[0].What != "power.inhibitors" || r.List[0].Reason != c.reason {
			t.Errorf("owner %q: %+v, %+v", c.owner, in.Logind, r.List)
		}
	}
	// Con dueño pero sin respuesta de ListInhibitors: error también, no pánico.
	r := &collect.Report{Lang: "en"}
	Inhibit(inhibitEnv(t, `[true]`, "", ""), r)
	if len(r.List) != 1 || r.List[0].What != "power.inhibitors" || r.List[0].Reason != "error" {
		t.Errorf("call fails: %+v", r.List)
	}
}

// M16: un Env armado a mano sin Bus no entra en pánico; se trata como
// logind ausente.
func TestInhibitorsNilBus(t *testing.T) {
	env := source.Env{FS: source.FS{Root: t.TempDir()}, Lang: "en", Home: "/home/user"}
	r := &collect.Report{Lang: "en"}
	in := Inhibit(env, r)
	if in == nil || len(in.Logind) != 0 || len(in.Windows) != 0 {
		t.Fatalf("nil bus: %+v", in)
	}
	if len(r.List) != 1 || r.List[0].What != "power.inhibitors" || r.List[0].Reason != "not_present" {
		t.Fatalf("nil bus unavailable: %+v", r.List)
	}
}

func TestIdleWindows(t *testing.T) {
	// El fixture apps: Steam tiene inhibitingIdle.
	env := inhibitEnv(t, `[false]`, "", "")
	env.Hypr = &source.FakeHypr{Dir: filepath.Join(source.FixturesDir(t), "apps", "hypr")}
	r := &collect.Report{Lang: "en"}
	in := Inhibit(env, r)
	if !slices.Equal(in.Windows, []model.IdleWindow{{Class: "steam", Title: "Steam"}}) {
		t.Fatalf("windows = %+v", in.Windows)
	}
	// Sin mapear no cuenta; un título hostil se limpia.
	clients := `[{"class":"mpv","title":"movie\u001b]0;x\u0007.mkv","mapped":true,"inhibitingIdle":true},
	 {"class":"ghost","title":"g","mapped":false,"inhibitingIdle":true},
	 {"class":"kitty","title":"~","mapped":true,"inhibitingIdle":false}]`
	in = Inhibit(inhibitEnv(t, `[false]`, "", clients), &collect.Report{Lang: "en"})
	if !slices.Equal(in.Windows, []model.IdleWindow{{Class: "mpv", Title: "movie]0;x.mkv"}}) {
		t.Fatalf("windows = %+v", in.Windows)
	}
	// Sin Hyprland: sin ventanas y sin unavailable (no es un error).
	r = &collect.Report{Lang: "en"}
	env = inhibitEnv(t, `[false]`, "", "")
	env.Hypr = nil
	if in := Inhibit(env, r); len(in.Windows) != 0 || in.Windows == nil || len(r.List) != 1 {
		t.Fatalf("without Hyprland: %+v, %+v", in.Windows, r.List) // el único unavailable es el de logind
	}
	// Una respuesta que no es JSON: error propio.
	r = &collect.Report{Lang: "en"}
	Inhibit(inhibitEnv(t, `[false]`, "", "not json"), r)
	if len(r.List) != 2 || r.List[1].What != "power.idle_windows" || r.List[1].Reason != "error" {
		t.Fatalf("garbage: %+v", r.List)
	}
}

func TestStayAwake(t *testing.T) {
	env := inhibitEnv(t, `[false]`, "", "")
	if Inhibit(env, &collect.Report{}).StayAwake {
		t.Fatal("stay-awake without the file")
	}
	p := filepath.Join(env.FS.Root, "home/user/.local/state/omarchy/indicators/stay-awake")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, nil, 0o644)
	if !Inhibit(env, &collect.Report{}).StayAwake {
		t.Fatal("stay-awake with the file")
	}
	env.Home = ""
	if Inhibit(env, &collect.Report{}).StayAwake {
		t.Fatal("stay-awake without a home")
	}
}
