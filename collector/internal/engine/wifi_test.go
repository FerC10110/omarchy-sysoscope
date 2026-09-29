package engine

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// nmRunner contesta nmcli desde el fixture wifi y lo demás desde el de la
// máquina. reads cuenta las lecturas (su primera orden es `nmcli -t radio
// wifi`); con block, esa orden espera hasta que se cierre.
type nmRunner struct {
	*source.FakeRunner
	nm    *source.FakeRunner
	block chan struct{}
	reads atomic.Int32
}

func (r *nmRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if name != "nmcli" {
		return r.FakeRunner.Run(ctx, name, args...)
	}
	if len(args) == 3 && args[1] == "radio" {
		r.reads.Add(1)
		if r.block != nil {
			<-r.block
		}
	}
	return r.nm.Run(ctx, name, args...)
}

func withNM(t *testing.T, env source.Env) (source.Env, *nmRunner) {
	t.Helper()
	nr := &nmRunner{FakeRunner: runs(env), nm: &source.FakeRunner{Dir: filepath.Join(source.FixturesDir(t), "wifi", "cmd")}}
	env.Run = nr
	return env, nr
}

func wlp(s *model.Snapshot) *model.Wifi {
	for _, i := range s.Network.Interfaces {
		if i.Name == "wlp3s0" {
			return i.Wifi
		}
	}
	return nil
}

// Sin la vista Red no corre nmcli; con ella, una lectura cada 10 s; al salir
// se borra lo leído.
func TestWifiOnlyWithNetworkFocus(t *testing.T) {
	env, nr := withNM(t, source.Fixture(t, "threadripper"))
	now := source.FixtureNow
	env.Now = func() time.Time { return now }
	e := newEngine(t, env, "about")
	for range 6 {
		e.Tick()
		now = now.Add(2 * time.Second)
	}
	if n := nr.nm.Count("nmcli"); n != 0 {
		t.Fatalf("nmcli ran %d times without the Network view", n)
	}
	e.SetFocus("network")
	var s *model.Snapshot
	for range 10 { // 20 s: lee al entrar y a los 10 s
		s = e.Tick()
		now = now.Add(2 * time.Second)
	}
	if n := nr.reads.Load(); n != 2 {
		t.Fatalf("reads in 20 s = %d, want 2", n)
	}
	if w := wlp(s); w == nil || w.Manager != "nm" || len(w.Networks) != 6 || w.Radio != "on" {
		t.Fatalf("wlp3s0 = %+v", w)
	}
	e.SetFocus("")
	if w := wlp(e.Tick()); w != nil && w.Manager != "" {
		t.Fatalf("Wi-Fi kept after leaving Network: %+v", w)
	}
	e.RefreshWifi() // sin la vista Red no lee igual
	e.Tick()
	if n := nr.reads.Load(); n != 2 {
		t.Fatalf("reads after leaving = %d, want 2", n)
	}
}

// Una acción de otro proceso (lazylazy, o un wifi-wait desacoplado) deja su
// rastro en actions.log; con la vista Red abierta, wifiTick lo nota (mtime o
// tamaño) y relee sin esperar los 10 s. Sin cambios en el log no relee, y sin
// la vista Red no mira el log ni relee.
func TestWifiTickWatchesActionsLog(t *testing.T) {
	env, nr := withNM(t, source.Fixture(t, "threadripper"))
	e := newEngine(t, env, "network") // el reloj del fixture no avanza
	e.Tick()                          // primera lectura
	e.Tick()                          // sin cambios: no relee
	if n := nr.reads.Load(); n != 1 {
		t.Fatalf("reads = %d, want 1 before touching actions.log", n)
	}

	logPath := filepath.Join(env.State, "actions.log")
	if err := os.WriteFile(logPath, []byte("line one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.Tick() // ve el log por primera vez: registra la base, todavía no relee
	if n := nr.reads.Load(); n != 1 {
		t.Fatalf("reads = %d, want 1 right after the log appears", n)
	}

	e.Tick() // sin cambios en el log: no relee
	if n := nr.reads.Load(); n != 1 {
		t.Fatalf("reads = %d, want 1 with an unchanged log", n)
	}

	if err := os.WriteFile(logPath, []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.Tick() // una acción de otro proceso: relee aunque no pasaron los 10 s
	if n := nr.reads.Load(); n != 2 {
		t.Fatalf("reads after actions.log changed = %d, want 2", n)
	}
}

func TestWifiActionsLogIgnoredWithoutNetworkFocus(t *testing.T) {
	env, nr := withNM(t, source.Fixture(t, "threadripper"))
	e := newEngine(t, env, "") // sin la vista Red
	logPath := filepath.Join(env.State, "actions.log")
	if err := os.WriteFile(logPath, []byte("line one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.Tick()
	if err := os.WriteFile(logPath, []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.Tick()
	if n := nr.reads.Load(); n != 0 {
		t.Fatalf("nmcli read %d times without the Network view", n)
	}
}

func TestRefreshWifiReadsNow(t *testing.T) {
	env, nr := withNM(t, source.Fixture(t, "threadripper"))
	e := newEngine(t, env, "network") // el reloj del fixture no avanza
	e.Tick()
	e.Tick()
	e.RefreshWifi()
	e.Tick()
	if n := nr.reads.Load(); n != 2 {
		t.Fatalf("reads = %d, want 2 (first tick and after RefreshWifi)", n)
	}
}

// En el stream la lectura va en su gorutina: Tick no espera a nmcli y no se
// lanza otra mientras una sigue en curso.
func TestWifiReadDoesNotBlockTick(t *testing.T) {
	env, nr := withNM(t, source.Fixture(t, "threadripper"))
	nr.block = make(chan struct{})
	now := source.FixtureNow
	env.Now = func() time.Time { return now }
	e := New(env, Options{Interval: 2 * time.Second, Focus: "network", Stream: true, Statfs: fakeStatfs})
	defer e.Close()
	done := make(chan *model.Snapshot)
	go func() {
		var s *model.Snapshot
		for range 12 { // 24 s con la lectura trabada
			s = e.Tick()
			now = now.Add(2 * time.Second)
		}
		done <- s
	}()
	var s *model.Snapshot
	select {
	case s = <-done:
	case <-time.After(5 * time.Second):
		close(nr.block)
		t.Fatal("Tick waited for nmcli")
	}
	if w := wlp(s); w != nil && w.Manager != "" {
		t.Fatalf("Wi-Fi before nmcli answered: %+v", w)
	}
	if n := nr.reads.Load(); n != 1 {
		t.Fatalf("reads = %d, want 1 while the first one is running", n)
	}
	close(nr.block)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if w := wlp(e.Tick()); w != nil && w.Manager == "nm" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the Wi-Fi read never arrived")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
