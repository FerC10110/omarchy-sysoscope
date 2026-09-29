package engine

import (
	"slices"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

func hyprCalls(env source.Env, cmd string) int {
	n := 0
	for _, c := range env.Hypr.(*source.FakeHypr).Calls {
		if c == cmd {
			n++
		}
	}
	return n
}

// El modo de suspensión y lo que despierta van con lo lento, aunque
// Energía esté cerrada.
func TestSleepAndWakeWithTheSlowRead(t *testing.T) {
	s := newEngine(t, source.Fixture(t, "ultrawide"), "").Tick()
	if s.Power.SleepMode != "deep" || !slices.Equal(s.Power.SleepModes, []string{"s2idle", "deep"}) {
		t.Fatalf("sleep = %q %q", s.Power.SleepMode, s.Power.SleepModes)
	}
	want := []model.WakeDevice{{Name: "USB Receiver", Kind: "usb", Path: "1-5"}, {Name: "2.4G Wireless Keyboard", Kind: "usb", Path: "1-6"},
		{Name: "Intel Corporation I211 Gigabit Network Connection", Kind: "net", Path: "0000:05:00.0"}}
	if !slices.Equal(s.Power.Wakeup, want) {
		t.Fatalf("wakeup = %+v", s.Power.Wakeup)
	}
	if u := s.System.USB; len(u) != 2 || u[0].Version != "2.00" || !u[0].SSPort || u[0].Class != "03" {
		t.Fatalf("usb = %+v", u)
	}
}

// Los inhibidores solo con Energía abierta, en cada muestra; al salir se
// borran (no se muestran viejos).
func TestInhibitOnlyWithPowerFocus(t *testing.T) {
	env := source.Fixture(t, "laptop")
	bus := env.Bus.(*source.FakeBus)
	e := newEngine(t, env, "")
	if s := e.Tick(); s.Power.Inhibit != nil || bus.Called("ListInhibitors") != 0 {
		t.Fatalf("closed: %+v, %d calls", s.Power.Inhibit, bus.Called("ListInhibitors"))
	}
	e.SetFocus("power")
	s := e.Tick()
	if in := s.Power.Inhibit; in == nil || len(in.Logind) != 2 || in.Logind[0].Mode != "block" {
		t.Fatalf("open: %+v", s.Power.Inhibit)
	}
	e.Tick()
	if n := bus.Called("ListInhibitors"); n != 2 {
		t.Fatalf("every sample with Power open: %d calls", n)
	}
	e.SetFocus("about")
	if s := e.Tick(); s.Power.Inhibit != nil || bus.Called("ListInhibitors") != 2 {
		t.Fatalf("after leaving: %+v", s.Power.Inhibit)
	}
	if s.Power.SleepMode != "s2idle" {
		t.Fatalf("laptop sleep = %q", s.Power.SleepMode)
	}
}

// El modo en uso de los monitores: con lo lento y, con Dispositivos
// abierta, cada everyNet muestras.
func TestMonitorsLiveCadence(t *testing.T) {
	env := source.Fixture(t, "ultrawide")
	e := newEngine(t, env, "")
	var s *model.Snapshot
	for range 6 {
		s = e.Tick()
	}
	if n := hyprCalls(env, "j/monitors"); n != 1 {
		t.Fatalf("closed: %d reads of j/monitors, want 1 (the slow read)", n)
	}
	m := s.System.Monitors[0]
	if m.Connector != "HDMI-A-1" || m.MaxVHz != 75 || m.Live == nil || m.Live.RefreshHz != 49.987 || m.Live.BestHz != 49.99 {
		t.Fatalf("monitor = %+v live %+v", m, m.Live)
	}
	env2 := source.Fixture(t, "ultrawide")
	e2 := newEngine(t, env2, "devices")
	for range 6 {
		e2.Tick()
	}
	if n := hyprCalls(env2, "j/monitors"); n != 2 {
		t.Fatalf("with Devices open: %d reads, want 2 (ticks 0 and 5)", n)
	}
}

// Al entrar a Dispositivos, la muestra siguiente relee el modo en uso (no
// espera hasta everyNet muestras); quedarse en ella no vuelve a forzarlo.
func TestMonitorsLiveOnEnteringDevices(t *testing.T) {
	env := source.Fixture(t, "ultrawide")
	e := newEngine(t, env, "")
	e.Tick() // tick 0: lo lento
	e.Tick() // tick 1
	e.SetFocus("devices")
	e.Tick() // tick 2: se acaba de entrar
	if n := hyprCalls(env, "j/monitors"); n != 2 {
		t.Fatalf("after entering Devices: %d reads, want 2", n)
	}
	e.SetFocus("devices") // la misma sección: no es entrar
	e.Tick()              // tick 3
	e.Tick()              // tick 4
	if n := hyprCalls(env, "j/monitors"); n != 2 {
		t.Fatalf("staying in Devices: %d reads, want 2", n)
	}
	e.Tick() // tick 5: everyNet
	if n := hyprCalls(env, "j/monitors"); n != 3 {
		t.Fatalf("tick 5 with Devices open: %d reads, want 3", n)
	}
}
