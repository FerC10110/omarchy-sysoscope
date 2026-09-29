package engine

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/apps"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// clients cuenta las lecturas de ventanas: una por lectura de las apps.
func clients(env source.Env) int {
	n := 0
	for _, c := range env.Hypr.(*source.FakeHypr).Calls {
		if c == "j/clients" {
			n++
		}
	}
	return n
}

// Las apps (recorrer /proc, Hyprland, ss, GPU) se leen solo con una vista que
// las muestra: Aplicaciones cada 2 muestras; CPU, Memoria, GPU y Red cada 5.
// Al entrar a una de ellas se leen enseguida.
func TestAppsCadence(t *testing.T) {
	env := source.Fixture(t, "apps")
	e := newEngine(t, env, "")
	var s *model.Snapshot
	for i := 0; i < 6; i++ {
		s = e.Tick()
	}
	if n := clients(env); n != 0 {
		t.Errorf("without a view that shows apps: %d reads, want 0", n)
	}
	if n := runs(env).Count(source.Key("ss", apps.SSArgs...)); n != 0 {
		t.Errorf("ss = %d without a view that shows apps, want 0", n)
	}
	if s.Apps == nil || len(s.Apps.List) != 0 {
		t.Fatalf("apps without a view = %+v, want an empty list", s.Apps)
	}

	e.SetFocus("memory") // tick 6: se lee al entrar, con ss
	s = e.Tick()
	if n := clients(env); n != 1 || len(s.Apps.List) == 0 {
		t.Fatalf("entering memory: %d reads, %d apps", n, len(s.Apps.List))
	}
	if n := runs(env).Count(source.Key("ss", apps.SSArgs...)); n != 1 {
		t.Errorf("ss = %d when entering memory, want 1", n)
	}
	// Tick 7: la primera lectura no da tasas, se vuelve a leer enseguida;
	// después solo el 10.
	for i := 0; i < 4; i++ {
		e.Tick()
	}
	if n := clients(env); n != 3 {
		t.Errorf("memory open for 5 ticks: %d reads, want 3", n)
	}

	e.SetFocus("apps") // tick 11: al entrar (la base es de hace 2 s); 12 y 14, cada 2
	for i := 0; i < 4; i++ {
		e.Tick()
	}
	if n := clients(env); n != 6 {
		t.Errorf("apps open for 4 ticks: %d reads in total, want 6", n)
	}

	e.SetFocus("") // el panel se cierra: no se lee más, la lista queda vacía
	for i := 0; i < 6; i++ {
		s = e.Tick()
	}
	if n := clients(env); n != 6 || len(s.Apps.List) != 0 {
		t.Errorf("after closing: %d reads, %d apps", n, len(s.Apps.List))
	}
}

// Al volver a una vista después de un rato, la lectura anterior es vieja:
// las tasas saldrían promediadas sobre todo ese tiempo. Esa lectura cuenta
// como la primera (sin tasas) y se vuelve a leer en la muestra siguiente.
func TestAppsStaleBaseline(t *testing.T) {
	env := source.Fixture(t, "apps")
	now := source.FixtureNow
	env.Now = func() time.Time { return now }
	tick := func(e *Engine) *model.Snapshot { now = now.Add(2 * time.Second); return e.Tick() }
	e := newEngine(t, env, "network")
	tick(e)
	tick(e)
	if s := tick(e); s.Apps.List[0].NetRxBps == nil {
		t.Fatal("no network rate with a fresh baseline")
	}
	e.SetFocus("")
	tick(e)
	now = now.Add(time.Hour)
	e.SetFocus("network")
	before := clients(env)
	s := tick(e)
	if s.Apps.List[0].NetRxBps != nil {
		t.Errorf("entry read after an hour has a rate: %v", *s.Apps.List[0].NetRxBps)
	}
	s = tick(e)
	if n := clients(env) - before; n != 2 || s.Apps.List[0].NetRxBps == nil {
		t.Errorf("after the stale entry: %d reads, rate %v; want 2 reads and a rate", n, s.Apps.List[0].NetRxBps)
	}
}

// La barra muestra lo congelado aunque nadie mire las apps: sale del
// freezer, no de recorrer /proc.
func TestFrozenWithoutAppsView(t *testing.T) {
	env := source.Fixture(t, "apps")
	until := source.FixtureNow.Add(time.Minute).Format(time.RFC3339)
	os.WriteFile(filepath.Join(env.State, "frozen.json"), []byte(`[{"id":"comm:syncthing","name":"syncthing","until":"`+until+`"}]`), 0o644)
	s := newEngine(t, env, "").Tick()
	if s.Apps == nil || len(s.Apps.Frozen) != 1 || s.Apps.Frozen[0].Name != "syncthing" {
		t.Fatalf("frozen = %+v", s.Apps)
	}
	if n := clients(env); n != 0 {
		t.Fatalf("%d reads of the apps", n)
	}
}

func TestEngineMarksFrozenApps(t *testing.T) {
	env := source.Fixture(t, "apps")
	until := source.FixtureNow.Add(time.Minute).Format(time.RFC3339)
	gone := source.FixtureNow.Add(-time.Minute).Format(time.RFC3339)
	os.WriteFile(filepath.Join(env.State, "frozen.json"), []byte(`[{"id":"comm:syncthing","name":"syncthing","until":"`+until+`"},`+
		`{"id":"comm:old","name":"old","until":"`+gone+`"}]`), 0o644)
	s := newEngine(t, env, "apps").Tick()
	var sync model.App
	for _, a := range s.Apps.List {
		if a.ID == "comm:syncthing" {
			sync = a
		}
	}
	if !sync.Frozen || len(s.Apps.Frozen) != 1 || s.Apps.Frozen[0].ID != "comm:syncthing" {
		t.Fatalf("frozen = %+v / %+v", sync, s.Apps.Frozen)
	}
	if runs(env).Count("notify-send") != 1 {
		t.Fatal("the expired entry was not resumed with a notification")
	}
}

func engineApp(s *model.Snapshot, id string) model.App {
	for _, a := range s.Apps.List {
		if a.ID == id {
			return a
		}
	}
	return model.App{}
}

// El disco por app se lee solo con Aplicaciones abierta: en CPU (y Memoria,
// GPU, Red) las apps salen sin él. Al entrar, la primera lectura no tiene
// base; la siguiente sí. Al salir, se deja de leer. (Que con Disk=false no
// haya lecturas de /proc/<pid>/io lo cuenta apps.TestSampleNoDiskWithoutFlag;
// aquí se comprueba la bandera del Sampler y sus efectos.)
func TestDiskOnlyWithApps(t *testing.T) {
	env := source.Fixture(t, "apps")
	now := source.FixtureNow
	env.Now = func() time.Time { return now }
	tick := func(e *Engine) *model.Snapshot { now = now.Add(2 * time.Second); return e.Tick() }
	e := newEngine(t, env, "cpu")
	if e.appsS.Disk {
		t.Fatal("Disk on with CPU open")
	}
	var s *model.Snapshot
	for i := 0; i < 6; i++ {
		s = tick(e)
		for _, a := range s.Apps.List {
			if a.DiskReadBps != nil || a.DiskWriteBps != nil {
				t.Fatalf("tick %d: %s has disk data with CPU open", i, a.ID)
			}
		}
	}
	e.SetFocus("apps")
	if !e.appsS.Disk {
		t.Fatal("Disk off with Applications open")
	}
	s = tick(e) // se lee al entrar: la lectura anterior fue sin disco
	if a := engineApp(s, "comm:syncthing"); a.DiskReadBps != nil {
		t.Fatal("a disk rate on entering Applications")
	}
	got := false
	for i := 0; i < 3 && !got; i++ {
		a := engineApp(tick(e), "comm:syncthing")
		// El fixture no cambia: la tasa es 0, pero hay dato.
		got = a.DiskReadBps != nil && *a.DiskReadBps == 0 && a.DiskWriteBps != nil && *a.DiskWriteBps == 0
	}
	if !got {
		t.Fatal("no disk data after 3 samples on Applications")
	}
	e.SetFocus("cpu") // se lee al entrar, ya sin disco
	if e.appsS.Disk {
		t.Fatal("Disk on after leaving Applications")
	}
	s = tick(e)
	if a := engineApp(s, "comm:syncthing"); a.DiskReadBps != nil || a.DiskWriteBps != nil {
		t.Fatal("disk data after leaving Applications")
	}
}

// Aplicaciones → Resumen → Aplicaciones en menos de 20 s (sin Reset por base
// vieja): la primera lectura al volver no tiene disco (no promedia el rato
// afuera) y la CPU sigue con su tasa.
func TestDiskBaseDroppedLeavingApps(t *testing.T) {
	env := source.FixtureCopy(t, "apps")
	now := source.FixtureNow
	env.Now = func() time.Time { return now }
	// Chromium (pid 200) gasta CPU en cada intervalo: sin base, su tasa sería 0.
	utime := 10000
	tick := func(e *Engine) *model.Snapshot {
		now = now.Add(2 * time.Second)
		utime += 200
		p := env.FS.Real("/proc/200/stat")
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		f := strings.Fields(string(b))
		f[13] = strconv.Itoa(utime)
		if err := os.WriteFile(p, []byte(strings.Join(f, " ")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return e.Tick()
	}
	e := newEngine(t, env, "apps")
	e.cpu.Threads = 1 // el fixture no trae hilos y la tasa de CPU de las apps los necesita
	var s *model.Snapshot
	has := false
	for i := 0; i < 8 && !has; i++ {
		s = tick(e)
		has = engineApp(s, "comm:syncthing").DiskReadBps != nil
	}
	if !has {
		t.Fatal("no disk data on Applications")
	}
	e.SetFocus("summary")
	tick(e)
	tick(e)
	e.SetFocus("apps")
	s = tick(e) // 6 s desde la última lectura: no hay Reset por base vieja
	if a := engineApp(s, "comm:syncthing"); a.DiskReadBps != nil || a.DiskWriteBps != nil {
		t.Fatalf("disk rate on the first sample back: %v / %v", a.DiskReadBps, a.DiskWriteBps)
	}
	if len(s.Apps.List) == 0 {
		t.Fatal("no apps on the first sample back")
	}
	// La CPU conserva su base: hay tasa (no 0 por falta de base) para chromium.
	if a := engineApp(s, "app-org.chromium.Chromium-200.scope"); a.CPUPct == 0 {
		t.Fatal("CPU base lost")
	}
}
