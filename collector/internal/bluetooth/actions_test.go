package bluetooth

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// laptopObjects: un adaptador y tres dispositivos (auriculares conectados,
// teclado emparejado, parlante cercano).
func laptopObjects(powered bool) map[string]any {
	return map[string]any{
		"/org/bluez/hci0":                       adapter(powered),
		"/org/bluez/hci0/dev_02_00_00_00_00_0A": device("02:00:00:00:00:0A", "WH-1000XM4", true, true),
		"/org/bluez/hci0/dev_02_00_00_00_00_0B": device("02:00:00:00:00:0B", "Keyboard K380", false, true),
		"/org/bluez/hci0/dev_02_00_00_00_00_0C": device("02:00:00:00:00:0C", "Speaker", false, false),
	}
}

// setObjects cambia lo que BlueZ responde (lo que hizo el comando de verdad).
func setObjects(t *testing.T, bus *source.FakeBus, objs map[string]any) {
	t.Helper()
	raw, err := json.Marshal(objs)
	if err != nil {
		t.Fatal(err)
	}
	bus.System[objectsKey] = raw
}

// actionEnv: BlueZ con laptopObjects, los comandos de Omarchy y
// omarchy-shell en el PATH (/usr/bin de un FS temporal), un runner vacío y
// launch, executable y sleep reemplazados (launched anota lo lanzado).
func actionEnv(t *testing.T, powered bool) (source.Env, *source.FakeBus, *[][]string) {
	t.Helper()
	root := t.TempDir()
	for _, c := range []string{powerCmd, deviceCmd, "omarchy-shell"} {
		p := filepath.Join(root, "usr/bin", c)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755)
	}
	stubEnv(t, map[string]string{"PATH": "/usr/bin"})
	yes := true
	bus := btBus(t, laptopObjects(powered), &yes)
	env := source.Env{FS: source.FS{Root: root}, Run: &source.FakeRunner{Dir: t.TempDir()}, Bus: bus,
		Now: func() time.Time { return source.FixtureNow }, Lang: "en", State: t.TempDir()}
	var launched [][]string
	oldL, oldE, oldS := launch, executable, sleep
	launch = func(name string, args ...string) error {
		launched = append(launched, append([]string{name}, args...))
		return nil
	}
	executable = func() (string, error) { return "/opt/sysoscope", nil }
	sleep = func(time.Duration) {}
	t.Cleanup(func() { launch, executable, sleep = oldL, oldE, oldS })
	return env, bus, &launched
}

// put escribe la salida falsa de una orden en el directorio del runner.
func put(t *testing.T, env source.Env, out, name string, args ...string) {
	t.Helper()
	dir := env.Run.(*source.FakeRunner).Dir
	if err := os.WriteFile(filepath.Join(dir, source.Key(name, args...)+".txt"), []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
}

// putExit hace que la orden salga con code (para simular una falla de
// verdad, no solo un comando que no cambió nada).
func putExit(t *testing.T, env source.Env, code int, name string, args ...string) {
	t.Helper()
	dir := env.Run.(*source.FakeRunner).Dir
	if err := os.WriteFile(filepath.Join(dir, source.Key(name, args...)+".exit"), []byte(strconv.Itoa(code)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func actionsLog(t *testing.T, env source.Env) string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(env.State, "actions.log"))
	return string(b)
}

// hookRunner es un FakeRunner que, después de la orden key, llama a after
// (lo que la orden cambió de verdad).
type hookRunner struct {
	*source.FakeRunner
	key   string
	after func()
}

func (h *hookRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := h.FakeRunner.Run(ctx, name, args...)
	if source.Key(name, args...) == h.key {
		h.after()
	}
	return out, err
}

// deadlineRunner anota cuánto le queda al plazo y responde que venció.
type deadlineRunner struct{ left time.Duration }

func (r *deadlineRunner) Run(ctx context.Context, _ string, _ ...string) ([]byte, error) {
	d, _ := ctx.Deadline()
	r.left = time.Until(d)
	return nil, context.DeadlineExceeded
}

func TestKnown(t *testing.T) {
	for _, id := range []string{"bt.power", "bt.pair", "bt.connect", "bt.disconnect", "bt.forget"} {
		if !Known(id) {
			t.Errorf("Known(%q)", id)
		}
	}
	if Known("bt.nope") || Known("audio.mute") || !IsSection("bt.power") || !IsSection("bt.pair") || IsSection("bt.connect") {
		t.Fatal("Known / IsSection")
	}
}

// c, x y f se lanzan desacoplados (omarchy-bluetooth-device tarda hasta 20
// s y siempre sale 0): la acción responde enseguida y bt-wait anota el
// resultado de verdad. La dirección va como la da BlueZ.
func TestRowActionsLaunch(t *testing.T) {
	env, _, launched := actionEnv(t, true)
	for _, c := range []struct {
		id, target, out string
		argv            []string
	}{
		{"bt.connect", "02:00:00:00:00:0b", "Connecting to Keyboard K380…", []string{"connect", "02:00:00:00:00:0B"}},
		{"bt.disconnect", "02:00:00:00:00:0A", "Disconnecting from WH-1000XM4…", []string{"disconnect", "02:00:00:00:00:0A"}},
		{"bt.forget", "02:00:00:00:00:0B", "Forgetting Keyboard K380…", []string{"forget", "02:00:00:00:00:0B"}},
	} {
		n := len(*launched)
		out, err := Run(env, c.id, c.target, "")
		if err != nil || out != c.out || len(*launched) != n+1 {
			t.Fatalf("%s: out %q err %v launched %d", c.id, out, err, len(*launched))
		}
		want := append([]string{"/opt/sysoscope", "bt-wait", c.id, c.target, "--", deviceCmd}, c.argv...)
		if !slices.Equal((*launched)[n], want) {
			t.Fatalf("%s launched %q", c.id, (*launched)[n])
		}
	}
	// Sin la dirección ni el texto (nombra el dispositivo): ver actlog.
	if l := actionsLog(t, env); !strings.Contains(l, `bt.connect "" "" ok`) || strings.Contains(l, "02:00") || strings.Contains(l, "K380") {
		t.Fatalf("actions.log = %q", l)
	}
	if len(env.Run.(*source.FakeRunner).Calls) != 0 {
		t.Fatal("nothing runs in the action itself")
	}
}

// lazylazy manda cualquier tecla sobre cualquier fila: lo que no aplica se
// responde con el motivo y no se lanza nada.
func TestRowRefusals(t *testing.T) {
	env, _, launched := actionEnv(t, true)
	for _, c := range []struct{ id, target, err string }{
		{"bt.connect", "02:00:00:00:00:0A", "already connected to WH-1000XM4"},
		{"bt.connect", "02:00:00:00:00:0C", "Speaker is not paired: pair it with e (Omarchy's Bluetooth panel)"},
		{"bt.disconnect", "02:00:00:00:00:0B", "Keyboard K380 is not connected"},
		{"bt.forget", "02:00:00:00:00:0C", "Speaker is not paired"},
		{"bt.forget", "alsa_output.pci-0000_0c_00.3.analog-stereo", `"alsa_output.pci-0000_0c_00.3.analog-stereo" is not a Bluetooth device`},
		{"bt.connect", "", `"" is not a Bluetooth device`},
		{"bt.connect", "02:00:00:00:00:99", "no Bluetooth device 02:00:00:00:00:99"},
		{"bt.nope", "02:00:00:00:00:0A", `unknown action "bt.nope"`},
	} {
		if _, err := Run(env, c.id, c.target, ""); err == nil || err.Error() != c.err {
			t.Errorf("%s %q: %v", c.id, c.target, err)
		}
	}
	if len(*launched) != 0 {
		t.Fatalf("launched %q", *launched)
	}
}

func TestNoAdapterOrTools(t *testing.T) {
	env, bus, launched := actionEnv(t, true)
	setObjects(t, bus, map[string]any{})
	for _, id := range []string{"bt.power", "bt.pair", "bt.connect"} {
		if _, err := Run(env, id, "02:00:00:00:00:0B", ""); err == nil || err.Error() != "there is no Bluetooth adapter" {
			t.Errorf("%s without an adapter: %v", id, err)
		}
	}
	no := false
	env.Bus = btBus(t, laptopObjects(true), &no)
	if _, err := Run(env, "bt.power", "", ""); err == nil || err.Error() != "there is no Bluetooth adapter" {
		t.Errorf("BlueZ not running: %v", err)
	}
	env, _, launched = actionEnv(t, true)
	stubEnv(t, map[string]string{"PATH": "/nowhere"})
	for _, c := range []struct{ id, target, err string }{
		{"bt.power", "", "requires omarchy-bluetooth-power"},
		{"bt.pair", "", "requires omarchy-shell"},
		{"bt.connect", "02:00:00:00:00:0B", "requires omarchy-bluetooth-device"},
	} {
		if _, err := Run(env, c.id, c.target, ""); err == nil || err.Error() != c.err {
			t.Errorf("%s: %v", c.id, err)
		}
	}
	if len(*launched) != 0 {
		t.Fatalf("launched %q", *launched)
	}
}

// b decide según la lectura fresca: apagar si está encendido, si no prender.
func TestPower(t *testing.T) {
	env, _, launched := actionEnv(t, true)
	if out, err := Run(env, "bt.power", "", ""); err != nil || out != "Turning Bluetooth off…" ||
		!slices.Equal((*launched)[0], []string{"/opt/sysoscope", "bt-wait", "bt.power", "", "--", powerCmd, "off"}) {
		t.Fatalf("off: out %q err %v launched %q", out, err, *launched)
	}
	env, _, launched = actionEnv(t, false)
	if out, err := Run(env, "bt.power", "", ""); err != nil || out != "Turning Bluetooth on…" || (*launched)[0][6] != "on" {
		t.Fatalf("on: out %q err %v launched %q", out, err, *launched)
	}
}

// e abre el panel de Omarchy al momento (no pasa por bt-wait), también con
// el Bluetooth apagado: el panel lo prende.
func TestPair(t *testing.T) {
	env, _, launched := actionEnv(t, false)
	put(t, env, "", "omarchy-shell", "omarchy.bluetooth", "open")
	if out, err := Run(env, "bt.pair", "", ""); err != nil || out != "Opened Omarchy's Bluetooth panel" || len(*launched) != 0 {
		t.Fatalf("out %q err %v launched %q", out, err, *launched)
	}
	if calls := env.Run.(*source.FakeRunner).Calls; !slices.Equal(calls, []string{"omarchy-shell_omarchy.bluetooth_open"}) {
		t.Fatalf("calls = %q", calls)
	}
}

// Un nombre anunciado por un dispositivo cercano llega a la barra y a
// actions.log sin caracteres de control.
func TestPrintableNames(t *testing.T) {
	env, bus, _ := actionEnv(t, true)
	objs := laptopObjects(true)
	objs["/org/bluez/hci0/dev_02_00_00_00_00_0B"] = device("02:00:00:00:00:0B", "Evil\x1b]0;x\x07", false, true)
	setObjects(t, bus, objs)
	out, err := Run(env, "bt.connect", "02:00:00:00:00:0B", "")
	if err != nil || out != "Connecting to Evil]0;x…" || strings.ContainsAny(actionsLog(t, env), "\x1b\x07") {
		t.Fatalf("out %q err %v log %q", out, err, actionsLog(t, env))
	}
}

func TestWaitConnect(t *testing.T) {
	env, bus, _ := actionEnv(t, true)
	fake := env.Run.(*source.FakeRunner)
	put(t, env, "", deviceCmd, "connect", "02:00:00:00:00:0B")
	env.Run = &hookRunner{FakeRunner: fake, key: source.Key(deviceCmd, "connect", "02:00:00:00:00:0B"), after: func() {
		objs := laptopObjects(true)
		objs["/org/bluez/hci0/dev_02_00_00_00_00_0B"] = device("02:00:00:00:00:0B", "Keyboard K380", true, true)
		setObjects(t, bus, objs)
	}}
	if err := Wait(env, "bt.connect", "02:00:00:00:00:0b", []string{deviceCmd, "connect", "02:00:00:00:00:0B"}); err != nil {
		t.Fatal(err)
	}
	if l := actionsLog(t, env); l != "2026-09-23T12:00:00Z bt.connect \"\" \"\" ok\n" {
		t.Fatalf("actions.log = %q", l)
	}
}

// El comando no cambió nada (y salió 0 igual): se relee hasta 2 s y el
// resultado dice la verdad.
func TestWaitConnectFails(t *testing.T) {
	env, _, _ := actionEnv(t, true)
	var slept time.Duration
	sleep = func(d time.Duration) { slept += d }
	put(t, env, "", deviceCmd, "connect", "02:00:00:00:00:0B")
	err := Wait(env, "bt.connect", "02:00:00:00:00:0B", []string{deviceCmd, "connect", "02:00:00:00:00:0B"})
	if err == nil || err.Error() != "Could not connect to Keyboard K380" || slept != settle {
		t.Fatalf("err %v, slept %v", err, slept)
	}
	if l := actionsLog(t, env); !strings.HasSuffix(l, `bt.connect "" "" error`+"\n") {
		t.Fatalf("actions.log = %q", l)
	}
}

func TestWaitOutcomes(t *testing.T) {
	for _, c := range []struct {
		name, id, target string
		argv             []string
		powered          bool
		change           func(map[string]any)
		fail             bool // el comando salió con error de verdad (con motivo)
		want             string
		ok               bool
	}{
		{"disconnect", "bt.disconnect", "02:00:00:00:00:0A", []string{deviceCmd, "disconnect", "02:00:00:00:00:0A"}, true,
			func(o map[string]any) {
				o["/org/bluez/hci0/dev_02_00_00_00_00_0A"] = device("02:00:00:00:00:0A", "WH-1000XM4", false, true)
			}, false, "Disconnected from WH-1000XM4", true},
		{"disconnect fails", "bt.disconnect", "02:00:00:00:00:0A", []string{deviceCmd, "disconnect", "02:00:00:00:00:0A"}, true,
			func(map[string]any) {}, false, "Could not disconnect from WH-1000XM4", false},
		{"forget", "bt.forget", "02:00:00:00:00:0B", []string{deviceCmd, "forget", "02:00:00:00:00:0B"}, true,
			func(o map[string]any) { delete(o, "/org/bluez/hci0/dev_02_00_00_00_00_0B") }, false, "Forgot Keyboard K380", true},
		{"forget fails", "bt.forget", "02:00:00:00:00:0B", []string{deviceCmd, "forget", "02:00:00:00:00:0B"}, true,
			func(map[string]any) {}, false, "Could not forget Keyboard K380", false},
		// on: arranca apagado, el comando corre bien y BlueZ ya lo muestra
		// encendido cuando bt-wait lo relee.
		{"on", "bt.power", "", []string{powerCmd, "on"}, false,
			func(o map[string]any) { o["/org/bluez/hci0"] = adapter(true) }, false, "Bluetooth turned on", true},
		{"off", "bt.power", "", []string{powerCmd, "off"}, true,
			func(o map[string]any) { o["/org/bluez/hci0"] = adapter(false) }, false, "Bluetooth turned off", true},
		{"on fails", "bt.power", "", []string{powerCmd, "on"}, true,
			func(o map[string]any) { o["/org/bluez/hci0"] = adapter(false) }, false, "Could not turn Bluetooth on", false},
		// off fails with a reason: el comando salió con error de verdad (no
		// solo "no cambió nada"), y el motivo se suma al mensaje.
		{"off fails with reason", "bt.power", "", []string{powerCmd, "off"}, true,
			func(map[string]any) {}, true, "Could not turn Bluetooth off: Failed to power off: rfkill busy", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			env, bus, _ := actionEnv(t, c.powered)
			fake := env.Run.(*source.FakeRunner)
			out := ""
			if c.fail {
				out = "Failed to power off: rfkill busy\n"
			}
			put(t, env, out, c.argv[0], c.argv[1:]...)
			if c.fail {
				putExit(t, env, 1, c.argv[0], c.argv[1:]...)
			}
			env.Run = &hookRunner{FakeRunner: fake, key: source.Key(c.argv[0], c.argv[1:]...), after: func() {
				objs := laptopObjects(c.powered)
				c.change(objs)
				setObjects(t, bus, objs)
			}}
			// El texto no va a actions.log (ver actlog): se mira el de wait.
			out, err := wait(env, c.id, c.target, c.argv)
			if c.ok && (err != nil || out != c.want) {
				t.Fatalf("out %q err %v", out, err)
			}
			if !c.ok && (err == nil || err.Error() != c.want) {
				t.Fatalf("err %v", err)
			}
		})
	}
}

// bt-wait solo corre los dos comandos de Omarchy, y lo que no se puede
// correr queda anotado.
func TestWaitRefuses(t *testing.T) {
	env, _, _ := actionEnv(t, true)
	for _, argv := range [][]string{{"sh", "-c", "reboot"}, {}, {powerCmd, "toggle"}} {
		if err := Wait(env, "bt.power", "", argv); err == nil || err.Error() != "bt-wait only runs omarchy-bluetooth-device or omarchy-bluetooth-power" {
			t.Errorf("%q: %v", argv, err)
		}
	}
	// La orden tiene que ser exactamente la de esa acción y esa fila (verbo y
	// dirección incluidos): así el resultado de una no confirma el de otra.
	for _, c := range []struct {
		id, tg string
		argv   []string
	}{
		{"bt.connect", "02:00:00:00:00:0B", []string{deviceCmd, "disconnect", "02:00:00:00:00:0B"}},
		{"bt.connect", "02:00:00:00:00:0B", []string{deviceCmd, "connect", "02:00:00:00:00:0A"}},
		{"bt.connect", "02:00:00:00:00:0B", []string{deviceCmd, "connect", "not-an-address"}},
		{"bt.connect", "02:00:00:00:00:0B", []string{deviceCmd, "connect", "02:00:00:00:00:0B", "extra"}},
	} {
		if err := Wait(env, c.id, c.tg, c.argv); err == nil || err.Error() != "bt-wait only runs omarchy-bluetooth-device or omarchy-bluetooth-power" {
			t.Errorf("%s %q: %v", c.id, c.argv, err)
		}
	}
	if err := Wait(env, "bt.nope", "", []string{deviceCmd, "connect", "02:00:00:00:00:0B"}); err == nil || err.Error() != `unknown action "bt.nope"` {
		t.Errorf("unknown: %v", err)
	}
	if err := Wait(env, "bt.connect", "02:00:00:00:00:0B", []string{deviceCmd, "connect", "02:00:00:00:00:0B"}); err == nil ||
		err.Error() != "requires omarchy-bluetooth-device" {
		t.Errorf("missing command: %v", err)
	}
	if len(env.Run.(*source.FakeRunner).Calls) != 1 {
		t.Fatalf("calls = %q", env.Run.(*source.FakeRunner).Calls)
	}
	if l := actionsLog(t, env); strings.Count(l, "\" error\n") != 9 {
		t.Fatalf("actions.log = %q", l)
	}
}

// El plazo venció: el motivo ("timed out") se suma al mensaje.
func TestWaitTimeout(t *testing.T) {
	env, _, _ := actionEnv(t, true)
	r := &deadlineRunner{}
	env.Run = r
	if err := Wait(env, "bt.connect", "02:00:00:00:00:0B", []string{deviceCmd, "connect", "02:00:00:00:00:0B"}); err == nil ||
		err.Error() != "Could not connect to Keyboard K380: timed out" {
		t.Fatalf("err %v", err)
	}
	if r.left <= 29*time.Second || r.left > 30*time.Second {
		t.Fatalf("deadline in %v, want 30 s", r.left)
	}
}

func TestBluetoothActionsSpanish(t *testing.T) {
	env, _, _ := actionEnv(t, true)
	env.Lang = "es"
	if out, _ := Run(env, "bt.connect", "02:00:00:00:00:0B", ""); out != "Conectando a Keyboard K380…" {
		t.Fatalf("out %q", out)
	}
	put(t, env, "", deviceCmd, "connect", "02:00:00:00:00:0B")
	if err := Wait(env, "bt.connect", "02:00:00:00:00:0B", []string{deviceCmd, "connect", "02:00:00:00:00:0B"}); err == nil ||
		err.Error() != "No se pudo conectar a Keyboard K380" {
		t.Fatalf("err %v", err)
	}
}
