package engine

import (
	"encoding/json"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// Bluetooth va con lo lento, salvo con la vista Dispositivos abierta (cada
// muestra) o después de RefreshBluetooth (la muestra siguiente).
func TestBluetoothCadence(t *testing.T) {
	env := source.Fixture(t, "laptop")
	bus := env.Bus.(*source.FakeBus)
	key := "objects org.bluez /"
	orig := bus.System[key]
	e := newEngine(t, env, "")
	if s := e.Tick(); len(s.System.Bluetooth) != 3 || !s.System.BTPowered || !s.System.BTPresent {
		t.Fatalf("first tick: %+v", s.System.Bluetooth)
	}
	bus.System[key] = json.RawMessage(`{"/org/bluez/hci0": {"org.bluez.Adapter1": {"Powered": false}}}`)
	if s := e.Tick(); len(s.System.Bluetooth) != 3 {
		t.Fatal("without the Devices view Bluetooth waits for the slow read")
	}
	e.RefreshBluetooth()
	if s := e.Tick(); len(s.System.Bluetooth) != 0 || s.System.BTPowered || !s.System.BTPresent {
		t.Fatalf("after RefreshBluetooth: %+v", s.System)
	}
	bus.System[key] = orig
	if s := e.Tick(); len(s.System.Bluetooth) != 0 {
		t.Fatal("RefreshBluetooth reads once")
	}
	e.SetFocus("devices")
	if s := e.Tick(); len(s.System.Bluetooth) != 3 {
		t.Fatal("with the Devices view open every sample reads it")
	}
}
