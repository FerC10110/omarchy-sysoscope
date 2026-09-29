package engine

import (
	"encoding/json"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// El perfil va con lo lento, salvo con la vista Energía abierta (cada
// muestra) o después de RefreshPower (la muestra siguiente).
func TestPowerProfileCadence(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	bus := env.Bus.(*source.FakeBus)
	key := "net.hadess.PowerProfiles /net/hadess/PowerProfiles net.hadess.PowerProfiles ActiveProfile"
	e := newEngine(t, env, "")
	if p := e.Tick().Power.Profile; p != "balanced" {
		t.Fatalf("first tick: %q", p)
	}
	bus.System[key] = json.RawMessage(`"power-saver"`)
	if p := e.Tick().Power.Profile; p != "balanced" {
		t.Fatalf("without the Power view the profile waits for the slow read: %q", p)
	}
	e.RefreshPower()
	if p := e.Tick().Power.Profile; p != "power-saver" {
		t.Fatalf("after RefreshPower: %q", p)
	}
	bus.System[key] = json.RawMessage(`"balanced"`)
	if p := e.Tick().Power.Profile; p != "power-saver" {
		t.Fatalf("RefreshPower reads once: %q", p)
	}
	e.SetFocus("power")
	if p := e.Tick().Power.Profile; p != "balanced" {
		t.Fatalf("with the Power view open every sample reads it: %q", p)
	}
}
