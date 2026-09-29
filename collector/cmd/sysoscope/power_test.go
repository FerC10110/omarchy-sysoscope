package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/engine"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/view"
)

// stubPower reemplaza power.Run: power.profile_other falla, lo demás
// responde "ok <id>". El stream la llama desde goroutines: calls va con mutex.
func stubPower(t *testing.T) *[]runCall {
	t.Helper()
	var mu sync.Mutex
	var calls []runCall
	old := powerRun
	powerRun = func(_ source.Env, id, target, input string) (string, error) {
		mu.Lock()
		calls = append(calls, runCall{id, target, input})
		mu.Unlock()
		if id == "power.profile_other" {
			return "", errors.New("requires omarchy-powerprofiles-set")
		}
		return "ok " + id, nil
	}
	t.Cleanup(func() { powerRun = old })
	return &calls
}

// Las de Energía no llevan fila: `action power.profile performance` es el dato.
func TestActionCommandPower(t *testing.T) {
	calls := stubPower(t)
	wifi := stubWifi(t)
	a, _, out, errb := newApp(t, "")
	if code := a.run([]string{"action", "power.profile", "performance"}); code != 0 || out.String() != "ok power.profile\n" {
		t.Fatalf("profile: exit %d, out %q", code, out.String())
	}
	if code := a.run([]string{"action", "power.profile_other", "balanced"}); code != 1 || errb.String() != "requires omarchy-powerprofiles-set\n" {
		t.Fatalf("other: exit %d, err %q", code, errb.String())
	}
	want := []runCall{{"power.profile", "", "performance"}, {"power.profile_other", "", "balanced"}}
	if !slices.Equal(*calls, want) || len(*wifi) != 0 {
		t.Fatalf("power calls %+v, wifi calls %+v", *calls, *wifi)
	}
	if code := a.run([]string{"action", "power.nope", "x"}); code != 2 || len(*calls) != 2 {
		t.Fatal("an unknown power.* id must be refused before running")
	}
}

func TestLazylazyPowerAction(t *testing.T) {
	calls := stubPower(t)
	a, _, _, _ := newApp(t, "")
	if code := a.run([]string{"lazylazy", "action", "power.profile", "power-saver"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !slices.Equal(*calls, []runCall{{"power.profile", "", "power-saver"}}) {
		t.Fatalf("calls = %+v", *calls)
	}
}

// En el stream, el resultado sale en la línea siguiente y el engine relee el
// perfil (y no el Wi-Fi).
func TestStreamPowerAction(t *testing.T) {
	calls := stubPower(t)
	a, fake, _, _ := newApp(t, "")
	inW, lines, done := pipeStream(t, a)
	io.WriteString(inW, `action power.profile ["","performance"]`+"\n")
	got := waitResults(t, lines, 1)
	inW.Close()
	if code := <-done; code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !got[0].OK || got[0].Text != "ok power.profile" || got[0].ID != "power.profile" {
		t.Fatalf("result = %+v", got[0])
	}
	if !slices.Equal(*calls, []runCall{{"power.profile", "", "performance"}}) {
		t.Fatalf("calls = %+v", *calls)
	}
	if fake.powerRefresh != 1 || fake.wifiRefresh != 0 {
		t.Fatalf("RefreshPower %d, RefreshWifi %d", fake.powerRefresh, fake.wifiRefresh)
	}
}

// La vista Energía tiene su foco (relee el perfil en cada muestra), así que
// `view power` no reusa latest.json.
func TestPowerFocus(t *testing.T) {
	if engineFocus("power") != "power" || engineFocus("about") != "" {
		t.Fatal("engineFocus")
	}
	s := &model.Snapshot{Apps: &model.Apps{List: []model.App{{ID: "x"}}}}
	if latestServes(s, engineFocus("power")) {
		t.Fatal("latest.json must not serve the Power view")
	}
}

// lazylazy recibe p y P tal cual: de sección, en segundo plano, con su input.
func TestForLazylazyPower(t *testing.T) {
	in := &view.Input{Type: "choice", Prompt: "Profile", Options: []string{"balanced"}, Default: "balanced"}
	v := &view.View{Actions: []view.Action{
		{Key: "p", ID: "power.profile", Target: "section", Mode: "background", Input: in},
		{Key: "P", ID: "power.profile_other", Target: "section", Mode: "background", Input: in},
	}}
	for _, a := range forLazylazy(v).Actions {
		if a.Mode != "background" || a.Input == nil || a.Target != "section" {
			t.Fatalf("action = %+v", a)
		}
	}
}

// `snapshot --section power` fuerza un engine con Energía abierta (como
// maintenance y security): los inhibidores solo se leen con ese foco.
func TestSnapshotPowerSection(t *testing.T) {
	a, fake, _, _ := newApp(t, "")
	if code := a.run([]string{"snapshot", "--section", "power"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if len(fake.focus) != 1 || fake.focus[0] != "power" {
		t.Fatalf("focus = %q (the inhibitors are read only with the focus)", fake.focus)
	}
	// Con el engine de verdad sobre laptop: los dos inhibidores de logind.
	a, _, out, _ := newApp(t, "")
	a.env = source.Fixture(t, "laptop")
	a.newEngine = func(env source.Env, o engine.Options) sampler {
		o.Statfs = func(string) (uint64, uint64, uint64, error) { return 1, 1, 1, nil }
		return engine.New(env, o)
	}
	if code := a.run([]string{"snapshot", "--section", "power"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || got["cpu"] != nil {
		t.Fatalf("snapshot = %s", out.String())
	}
	var p model.Power
	if err := json.Unmarshal(got["power"], &p); err != nil || p.Inhibit == nil || len(p.Inhibit.Logind) != 2 ||
		p.Inhibit.Logind[0].Who != "prog1" || p.Inhibit.Logind[0].Mode != "block" {
		t.Fatalf("power = %s", got["power"])
	}
}

// inhibitEngine devuelve una muestra con datos que identifican: títulos,
// motivos y programas de los inhibidores, la clase de la ventana que mantiene
// la pantalla encendida, series de disco y monitor, hostname, MAC y el
// título de una ventana de una app.
type inhibitEngine struct {
	*fakeEngine
	last *model.Snapshot
}

var privateSentinels = []string{"SENTINEL-TITLE", "SENTINEL-WHY", "SENTINEL-WHO", "SENTINEL-CLASS", "SENTINEL-APPTITLE",
	"SENTINEL-DISKSERIAL", "SENTINEL-MONSERIAL", "SENTINEL-HOST", "02:00:5e:00:53:01"}

func (f *inhibitEngine) Tick() *model.Snapshot {
	s := f.fakeEngine.Tick()
	s.Power = &model.Power{Inhibit: &model.Inhibit{
		Logind:  []model.Inhibitor{{What: []string{"sleep"}, Who: "SENTINEL-WHO", Why: "SENTINEL-WHY", Mode: "block"}},
		Windows: []model.IdleWindow{{Class: "SENTINEL-CLASS", Title: "SENTINEL-TITLE"}}}}
	s.Storage = &model.Storage{Disks: []model.Disk{{Name: "nvme0n1", Model: "Disk Model", Serial: "SENTINEL-DISKSERIAL"}}}
	s.System = &model.System{Hostname: "SENTINEL-HOST", Monitors: []model.Monitor{{Connector: "DP-1", Model: "Mon Model", Serial: "SENTINEL-MONSERIAL"}}}
	s.Network = &model.Network{Interfaces: []model.Iface{{Name: "enp5s0", Kind: "ethernet", MAC: "02:00:5e:00:53:01"}}}
	s.Apps = &model.Apps{List: []model.App{{ID: "app-x.scope", Name: "X", Windows: []model.Window{{Address: "0xa1", Class: "x", Title: "SENTINEL-APPTITLE"}}}}}
	f.last = s
	return s
}

// latest.json (en disco, en cada muestra) no guarda nada que identifique
// (títulos, inhibidores, series, hostname, MAC); la línea del stream sí lo
// lleva, y la muestra del engine no se toca.
func TestStreamLatestWithoutPrivateData(t *testing.T) {
	a, fake, out, errb := newApp(t, "")
	ie := &inhibitEngine{fakeEngine: fake}
	a.newEngine = func(source.Env, engine.Options) sampler { return ie }
	inR, inW := io.Pipe()
	w := &firstLine{Buffer: out, ready: make(chan struct{})}
	a.stdin, a.stdout = inR, w
	go func() {
		<-w.ready
		io.WriteString(inW, "quit\n")
		inW.Close()
	}()
	t.Cleanup(func() { inW.Close() })
	if code := a.run([]string{"stream", "--interval", "1h"}); code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	for _, x := range privateSentinels {
		if !strings.Contains(out.String(), x) {
			t.Fatalf("stdout lost %q: %s", x, out.String())
		}
	}
	latest, err := os.ReadFile(filepath.Join(a.env.State, "latest.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range privateSentinels {
		if strings.Contains(string(latest), x) {
			t.Errorf("latest.json has %q", x)
		}
	}
	if s := string(latest); !strings.Contains(s, `"Disk Model"`) || !strings.Contains(s, `"Mon Model"`) {
		t.Errorf("latest.json lost the models: %s", s)
	}
	if in := ie.last.Power.Inhibit; in.Windows[0].Title != "SENTINEL-TITLE" || in.Logind[0].Who != "SENTINEL-WHO" ||
		ie.last.Storage.Disks[0].Serial != "SENTINEL-DISKSERIAL" || ie.last.Apps.List[0].Windows[0].Title != "SENTINEL-APPTITLE" {
		t.Fatalf("the engine's snapshot was changed: %+v", in)
	}
}
