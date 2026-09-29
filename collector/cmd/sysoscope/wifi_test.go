package main

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/view"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/wifi"
)

// stubWifi reemplaza wifi.Run: wifi.forget falla, lo demás responde "ok <id>".
// El stream la llama desde goroutines: calls va con mutex.
func stubWifi(t *testing.T) *[]runCall {
	t.Helper()
	var mu sync.Mutex
	var calls []runCall
	old := wifiRun
	wifiRun = func(_ source.Env, id, target, input string) (string, error) {
		mu.Lock()
		calls = append(calls, runCall{id, target, input})
		mu.Unlock()
		if id == "wifi.forget" {
			return "", errors.New("this network is not saved")
		}
		return "ok " + id, nil
	}
	t.Cleanup(func() { wifiRun = old })
	return &calls
}

// Las de sección no llevan fila: `action wifi.band 5` es el dato.
func TestActionCommandWifi(t *testing.T) {
	calls := stubWifi(t)
	apps := stubApps(t)
	a, _, out, errb := newApp(t, "")
	if code := a.run([]string{"action", "wifi.band", "5"}); code != 0 || out.String() != "ok wifi.band\n" {
		t.Fatalf("band: exit %d, out %q", code, out.String())
	}
	if code := a.run([]string{"action", "wifi.connect", "wlp3s0:Café: 2º piso"}); code != 0 {
		t.Fatalf("connect: exit %d", code)
	}
	if code := a.run([]string{"action", "wifi.forget", "wlp3s0:Plaza libre"}); code != 1 || errb.String() != "this network is not saved\n" {
		t.Fatalf("forget: exit %d, err %q", code, errb.String())
	}
	want := []runCall{{"wifi.band", "", "5"}, {"wifi.connect", "wlp3s0:Café: 2º piso", ""}, {"wifi.forget", "wlp3s0:Plaza libre", ""}}
	if !slices.Equal(*calls, want) || len(*apps) != 0 {
		t.Fatalf("wifi calls %+v, apps calls %+v", *calls, *apps)
	}
	if code := a.run([]string{"action", "wifi.nope"}); code != 2 || len(*calls) != 3 {
		t.Fatal("an unknown wifi.* id must be refused before running")
	}
}

func TestLazylazyWifiAction(t *testing.T) {
	calls := stubWifi(t)
	a, _, _, _ := newApp(t, "")
	if code := a.run([]string{"lazylazy", "action", "wifi.rescan"}); code != 0 {
		t.Fatalf("rescan: exit %d", code)
	}
	if code := a.run([]string{"lazylazy", "action", "wifi.disconnect", "wlp3s0:Casa"}); code != 0 {
		t.Fatalf("disconnect: exit %d", code)
	}
	want := []runCall{{"wifi.rescan", "", ""}, {"wifi.disconnect", "wlp3s0:Casa", ""}}
	if !slices.Equal(*calls, want) {
		t.Fatalf("calls = %+v", *calls)
	}
}

// En el stream, una acción de Wi-Fi responde en la línea siguiente y hace
// que el engine relea el Wi-Fi.
func TestStreamWifiAction(t *testing.T) {
	calls := stubWifi(t)
	a, fake, _, _ := newApp(t, "")
	inW, lines, done := pipeStream(t, a)
	io.WriteString(inW, `action wifi.band ["","6"]`+"\n")
	io.WriteString(inW, "action wifi.rescan\n")
	got := waitResults(t, lines, 2)
	inW.Close()
	if code := <-done; code != 0 {
		t.Fatalf("exit %d", code)
	}
	ok := map[string]bool{}
	for _, r := range got {
		ok[r.Text] = r.OK
	}
	if !ok["ok wifi.band"] || !ok["ok wifi.rescan"] {
		t.Fatalf("results = %+v", got)
	}
	want := map[runCall]bool{{"wifi.band", "", "6"}: true, {"wifi.rescan", "", ""}: true}
	if len(*calls) != 2 || !want[(*calls)[0]] || !want[(*calls)[1]] || (*calls)[0] == (*calls)[1] {
		t.Fatalf("calls = %+v", *calls)
	}
	if fake.wifiRefresh != 2 {
		t.Fatalf("RefreshWifi called %d times, want 2", fake.wifiRefresh)
	}
}

// lazylazy no tiene hide: una sola c, en pantalla completa (nmtui y
// wifi-hidden necesitan la terminal); connect_new y connect_hidden no van.
func TestForLazylazyWifi(t *testing.T) {
	v := &view.View{Actions: []view.Action{
		{Key: "c", ID: "wifi.connect", Target: "row", Mode: "background", Hide: []string{"connected", "locked"}},
		{Key: "c", ID: "wifi.connect_new", Target: "row", Mode: "background"},
		{Key: "c", ID: "wifi.connect_hidden", Target: "row", Mode: "background"},
		{Key: "d", ID: "wifi.disconnect", Target: "row", Mode: "background"},
		{Key: "f", ID: "firmware.check", Target: "section", Mode: "background"},
		{Key: "enter", ID: "window.focus", Target: "row", Mode: "background"},
	}}
	var got []string
	for _, a := range forLazylazy(v).Actions {
		got = append(got, a.Key+" "+a.ID+" "+a.Mode)
	}
	want := []string{"c wifi.connect fullscreen", "d wifi.disconnect background", "f firmware.check fullscreen", "g window.focus background"}
	if !slices.Equal(got, want) {
		t.Fatalf("actions = %q", got)
	}
}

func TestWifiWaitCommand(t *testing.T) {
	var got []string
	old := wifiWait
	wifiWait = func(_ source.Env, id, target, input string, argv []string) error {
		got = append([]string{id, target, input}, argv...)
		if argv[0] == "omarchy-network-band" {
			return errors.New("could not connect on 6")
		}
		return nil
	}
	t.Cleanup(func() { wifiWait = old })
	a, _, out, _ := newApp(t, "")
	args := []string{"wifi-wait", "wifi.connect", "wlp3s0:Casa", "", "--", "nmcli", "--wait", "45", "connection", "up", "uuid", "U", "ifname", "wlp3s0"}
	if code := a.run(args); code != 0 || out.Len() != 0 || !slices.Equal(got, append([]string{"wifi.connect", "wlp3s0:Casa", ""}, args[5:]...)) {
		t.Fatalf("exit %d, out %q, got %q", code, out.String(), got)
	}
	if code := a.run([]string{"wifi-wait", "wifi.band", "", "6", "--", "omarchy-network-band", "6"}); code != 1 {
		t.Fatalf("failed band: exit %d", code)
	}
	for _, bad := range [][]string{{"wifi-wait", "wifi.band"}, {"wifi-wait", "a", "b", "c", "nmcli"}, {"wifi-wait", "a", "b", "c", "--"}} {
		if code := a.run(bad); code != 2 {
			t.Errorf("%q: exit %d, want 2", bad, code)
		}
	}
}

// latest.json nunca sirve para Red: no guarda MACs, redes Wi-Fi ni IPs
// públicas (ver ondisk), ni con las apps y la lectura de Wi-Fi.
func TestLatestServes(t *testing.T) {
	s := &model.Snapshot{Apps: &model.Apps{List: []model.App{{ID: "x"}}},
		Network: &model.Network{Interfaces: []model.Iface{{Name: "enp5s0", Kind: "ethernet"},
			{Name: "wlp3s0", Kind: "wifi", Wifi: &model.Wifi{Manager: "nm"}}}}}
	if latestServes(s, "network") || !latestServes(s, "cpu") || !latestServes(s, "") || latestServes(s, "gpu") {
		t.Fatal("with the apps and the Wi-Fi read")
	}
	s.Apps = nil
	if latestServes(s, "network") || latestServes(s, "memory") {
		t.Fatal("without the apps")
	}
}

// El wifi-wait desacoplado es un proceso nuevo: tiene que resolver el mismo
// directorio de estado (dónde queda actions.log) y el mismo idioma que el
// proceso que lo lanzó, aunque ese idioma venga de un --lang o de
// LAZYLAZY_LANG que el hijo no recibe como tal (setLang lo deja en
// SYSOSCOPE_LANG para que startLang lo encuentre). env.State no tiene ese
// problema: sale siempre de XDG_STATE_HOME/HOME, que cualquier hijo hereda
// igual.
func TestWifiWaitResolvesParentStateAndLang(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("LANGUAGE", "")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv(sysoscopeLangEnv, "") // por si un test anterior lo dejó puesto

	// El padre (el stream con --lang es, o lazylazy con LAZYLAZY_LANG) elige
	// un idioma distinto del de las variables de entorno.
	parent := &app{env: source.Real(startLang())}
	parent.setLang("es")
	if parent.env.Lang != "es" {
		t.Fatalf("parent lang = %q, want es", parent.env.Lang)
	}

	var got source.Env
	old := wifiWait
	wifiWait = func(env source.Env, id, target, input string, argv []string) error {
		got = env
		return nil
	}
	t.Cleanup(func() { wifiWait = old })

	// El hijo es un proceso nuevo: misma construcción que main(), en el
	// mismo entorno (SYSOSCOPE_LANG ya quedó en "es").
	var out, errb strOut
	child := &app{env: source.Real(startLang()), stdout: &out, stderr: &errb}
	args := []string{"wifi-wait", "wifi.connect", "wlp3s0:Casa", "", "--", "nmcli", "--wait", "45"}
	if code := child.run(args); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errb.s)
	}
	if got.State != parent.env.State || got.Lang != parent.env.Lang {
		t.Fatalf("child env = %+v, parent env = %+v", got, parent.env)
	}
}

// strOut es un io.Writer mínimo para no arrastrar bytes.Buffer solo por esto.
type strOut struct{ s string }

func (o *strOut) Write(p []byte) (int, error) {
	o.s += string(p)
	return len(p), nil
}

// `sysoscope wifi-hidden [--hold] [--lang L] PLACA BSSID`: la terminal de
// este proceso (stdin, stdout) para ConnectHidden; --hold solo si vino.
func TestWifiHiddenCommand(t *testing.T) {
	type call struct {
		lang, iface, bssid, in string
		hold                   bool
	}
	var got []call
	old := wifiHidden
	wifiHidden = func(_ context.Context, env source.Env, iface, bssid string, in io.Reader, out io.Writer, hold bool) error {
		b, _ := io.ReadAll(in)
		got = append(got, call{env.Lang, iface, bssid, string(b), hold})
		io.WriteString(out, "prompt")
		switch bssid {
		case "AA:BB:CC:00:00:99":
			return &wifi.CancelError{Text: "cancelled"}
		case "AA:BB:CC:00:00:98":
			return errors.New("Secrets were required")
		}
		return nil
	}
	t.Cleanup(func() { wifiHidden = old })
	t.Setenv(sysoscopeLangEnv, "")
	a, _, out, _ := newApp(t, "Escondida\n")
	if code := a.run([]string{"wifi-hidden", "wlp3s0", "AA:BB:CC:00:00:05"}); code != 0 || out.String() != "prompt" {
		t.Fatalf("exit %d, out %q", code, out.String())
	}
	a, _, _, _ = newApp(t, "")
	if code := a.run([]string{"wifi-hidden", "--hold", "--lang", "es", "wlp3s0", "AA:BB:CC:00:00:99"}); code != 3 {
		t.Fatalf("cancelled: exit %d, want 3", code)
	}
	a, _, _, _ = newApp(t, "")
	if code := a.run([]string{"wifi-hidden", "--lang", "en", "wlp3s0", "AA:BB:CC:00:00:98"}); code != 1 {
		t.Fatalf("not connected: exit %d, want 1", code)
	}
	want := []call{{"en", "wlp3s0", "AA:BB:CC:00:00:05", "Escondida\n", false}, {"es", "wlp3s0", "AA:BB:CC:00:00:99", "", true}, {"en", "wlp3s0", "AA:BB:CC:00:00:98", "", false}}
	if !slices.Equal(got, want) {
		t.Fatalf("calls = %+v", got)
	}
	for _, bad := range [][]string{{"wifi-hidden"}, {"wifi-hidden", "wlp3s0"}, {"wifi-hidden", "--nope", "wlp3s0", "AA:BB:CC:00:00:05"}, {"wifi-hidden", "a", "b", "c"}} {
		a, _, _, errb := newApp(t, "")
		if code := a.run(bad); code != 2 || !strings.Contains(errb.String(), "usage") && !strings.Contains(errb.String(), "flag") {
			t.Errorf("%q: exit %d, stderr %q", bad, code, errb.String())
		}
	}
	if len(got) != 3 {
		t.Fatalf("a bad command ran: %+v", got)
	}
}
