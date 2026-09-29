package view

import (
	"slices"
	"strings"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

// devicesSnap es un Dispositivos inventado: dos salidas (la analógica
// predeterminada, el HDMI silenciado), una entrada al 150 %, Bluetooth
// encendido con los comandos de Omarchy, unos auriculares conectados, un
// teclado emparejado y un parlante cercano con un nombre hostil.
func devicesSnap(lang string) *model.Snapshot {
	bat := 70
	return &model.Snapshot{Lang: lang, System: &model.System{
		FailedUnits: []model.Unit{},
		Sound: &model.Sound{
			Outputs: []model.AudioNode{
				{ID: "56", Name: "alsa_output.analog", Label: "ALC1220 Analog", Volume: 80, Default: true},
				{ID: "88", Name: "alsa_output.hdmi", Label: "S34J55x", Volume: 40, Muted: true},
			},
			Inputs: []model.AudioNode{{ID: "57", Name: "alsa_input.analog", Label: "ALC1220 Analog", Volume: 150, Default: true}},
		},
		BTPresent: true, BTPowered: true, BTTools: true,
		Bluetooth: []model.BTDevice{
			{Name: "WH-1000XM4", Address: "02:00:00:00:00:0A", Connected: true, Paired: true, Battery: &bat},
			{Name: "Keyboard K380", Address: "02:00:00:00:00:0B", Paired: true},
			{Name: "Speaker\x1b[31m", Address: "02:00:00:00:00:0C"},
		},
	}}
}

func buildDevices(t *testing.T, s *model.Snapshot) *View {
	t.Helper()
	v, err := Build("devices", s, Options{Lang: s.Lang})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestDevicesSoundTables(t *testing.T) {
	v := buildDevices(t, devicesSnap("en"))
	out, in := table(v, "Outputs"), table(v, "Inputs")
	if out == nil || in == nil || !out.Selectable || !in.Selectable {
		t.Fatalf("tables: %+v %+v", out, in)
	}
	if !slices.Equal(out.Columns, []string{"Device", "Volume", "Muted", "Default"}) {
		t.Fatalf("columns = %q", out.Columns)
	}
	rs := tableRows(*out)
	if len(rs) != 2 || rs[0].ID != "alsa_output.analog" ||
		!slices.Equal(rs[0].Cells, []string{"ALC1220 Analog", "80%", "No", "✓"}) ||
		!slices.Equal(rs[0].Flags, []string{"audio", "output", "node:alsa_output.analog", "default"}) ||
		!slices.Equal(rs[1].Cells, []string{"S34J55x", "40%", "Yes", ""}) ||
		!slices.Equal(rs[1].Flags, []string{"audio", "output", "node:alsa_output.hdmi"}) {
		t.Fatalf("outputs = %+v", rs)
	}
	if r := tableRows(*in)[0]; r.Cells[1] != "150%" || !slices.Equal(r.Flags, []string{"audio", "input", "node:alsa_input.analog", "default"}) {
		t.Fatalf("input = %+v", r)
	}
	es := buildDevices(t, devicesSnap("es"))
	o := table(es, "Salidas")
	if o == nil || table(es, "Entradas") == nil || tableRows(*o)[1].Cells[2] != "Sí" || o.Columns[3] != "Predeterminada" {
		t.Fatalf("es = %+v", o)
	}
	// Sin la lectura de pactl: ni tablas ni acciones de audio.
	s := devicesSnap("en")
	s.System.Sound = nil
	v = buildDevices(t, s)
	if table(v, "Outputs") != nil || table(v, "Inputs") != nil || findAction(v, "audio.mute") != nil {
		t.Fatalf("without pactl: %+v", v)
	}
	noEmptyValues(t, v)
}

func TestDevicesBluetoothBlocks(t *testing.T) {
	v := buildDevices(t, devicesSnap("en"))
	if got := kvValue(v.Blocks, "Bluetooth", "State"); got != "Powered on" {
		t.Fatalf("state = %q", got)
	}
	bt := table(v, "Bluetooth devices")
	if bt == nil || !bt.Selectable || !slices.Equal(bt.Columns, []string{"Device", "State", "Battery"}) {
		t.Fatalf("table = %+v", bt)
	}
	rs := tableRows(*bt)
	want := []struct {
		id    string
		cells []string
		flags []string
	}{
		{"02:00:00:00:00:0A", []string{"WH-1000XM4", "Connected", "70%"}, []string{"bt", "connected", "paired"}},
		{"02:00:00:00:00:0B", []string{"Keyboard K380", "Paired", "—"}, []string{"bt", "disconnected", "paired"}},
		// Sin caracteres de control: un cercano anuncia el nombre que quiere.
		{"02:00:00:00:00:0C", []string{"Speaker[31m", "Nearby", "—"}, []string{"bt", "disconnected", "nearby"}},
	}
	for i, w := range want {
		if rs[i].ID != w.id || !slices.Equal(rs[i].Cells, w.cells) || !slices.Equal(rs[i].Flags, w.flags) {
			t.Errorf("row %d = %+v", i, rs[i])
		}
	}
	s := devicesSnap("es")
	s.System.BTPowered = false
	es := buildDevices(t, s)
	if got := kvValue(es.Blocks, "Bluetooth", "Estado"); got != "Apagado" || table(es, "Dispositivos Bluetooth") == nil {
		t.Fatalf("es off = %q", got)
	}
	// Sin adaptador: la fila de estado, sin tabla.
	s = devicesSnap("en")
	s.System.BTPresent, s.System.BTPowered, s.System.Bluetooth = false, false, nil
	v = buildDevices(t, s)
	if kvValue(v.Blocks, "Bluetooth", "State") != "No adapter" || table(v, "Bluetooth devices") != nil {
		t.Fatalf("no adapter: %+v", v.Blocks)
	}
	// Sin BlueZ: el motivo, como antes, sin fila de estado.
	s.Unavailable = []model.Unavailable{{What: "system.bluetooth", Reason: "not_present", Message: "not present on this system"}}
	v = buildDevices(t, s)
	if _, ok := texts(v)["not available: not present on this system"]; !ok || kvValue(v.Blocks, "Bluetooth", "State") != "" {
		t.Fatalf("without BlueZ: %+v", v.Blocks)
	}
	// Sin los comandos de Omarchy la tabla no se elige.
	s = devicesSnap("en")
	s.System.BTTools = false
	if table(buildDevices(t, s), "Bluetooth devices").Selectable {
		t.Fatal("without omarchy-bluetooth-* the table has no row actions")
	}
}

func TestDevicesActions(t *testing.T) {
	v := buildDevices(t, devicesSnap("en"))
	var got []string
	for _, a := range v.Actions {
		got = append(got, a.Key+" "+a.ID)
		if a.Mode != "background" {
			t.Errorf("%s: mode %q", a.ID, a.Mode)
		}
		if section := a.ID == "bt.power" || a.ID == "bt.pair"; (a.Target == "section") != section {
			t.Errorf("%s: target %q", a.ID, a.Target)
		}
	}
	want := []string{"d audio.default", "m audio.mute", "+ audio.up", "- audio.down", "v audio.volume", "v audio.volume", "v audio.volume",
		"b bt.power", "e bt.pair", "c bt.connect", "x bt.disconnect", "f bt.forget"}
	if !slices.Equal(got, want) {
		t.Fatalf("actions = %q", got)
	}
	d := findAction(v, "audio.default")
	if d.Label != "Make default" || !slices.Equal(d.Hide, []string{"bt", "default"}) {
		t.Fatalf("d = %+v", d)
	}
	for _, id := range []string{"audio.mute", "audio.up", "audio.down"} {
		if !slices.Equal(findAction(v, id).Hide, []string{"bt"}) {
			t.Errorf("%s hide = %q", id, findAction(v, id).Hide)
		}
	}
	// Una v por fila de audio, con el volumen de su fila (tope 100) y
	// escondida en las demás filas.
	names := []string{"alsa_output.analog", "alsa_output.hdmi", "alsa_input.analog"}
	defaults := []string{"80", "40", "100"}
	for i, a := range v.Actions[4:7] {
		if a.Input == nil || a.Input.Type != "number" || a.Input.Prompt != "Volume (0–100)" || a.Input.Default != defaults[i] || a.Label != "Set volume" {
			t.Errorf("v %d = %+v %+v", i, a, a.Input)
		}
		hide := []string{"bt"}
		for j, n := range names {
			if j != i {
				hide = append(hide, "node:"+n)
			}
		}
		if !slices.Equal(a.Hide, hide) {
			t.Errorf("v %d hide = %q", i, a.Hide)
		}
	}
	if p := findAction(v, "bt.power"); p.Label != "Turn Bluetooth off" {
		t.Fatalf("b = %+v", p)
	}
	if c := findAction(v, "bt.connect"); !slices.Equal(c.Hide, []string{"audio", "connected", "nearby"}) {
		t.Fatalf("c = %+v", c)
	}
	if x := findAction(v, "bt.disconnect"); !slices.Equal(x.Hide, []string{"audio", "disconnected"}) {
		t.Fatalf("x = %+v", x)
	}
	if f := findAction(v, "bt.forget"); f.Confirm != "Forget this device? It has to be paired again to use it." ||
		!slices.Equal(f.Hide, []string{"audio", "nearby"}) {
		t.Fatalf("f = %+v", f)
	}
	// Apagado: b enciende y olvidar avisa que prende el Bluetooth.
	s := devicesSnap("es")
	s.System.BTPowered = false
	v = buildDevices(t, s)
	if findAction(v, "bt.power").Label != "Encender Bluetooth" ||
		findAction(v, "bt.forget").Confirm != "¿Olvidar este dispositivo? Para eso se enciende el Bluetooth." {
		t.Fatalf("es off = %+v", v.Actions)
	}
	// Nada emparejado: solo b y e.
	s = devicesSnap("en")
	s.System.Bluetooth = s.System.Bluetooth[2:]
	v = buildDevices(t, s)
	if findAction(v, "bt.power") == nil || findAction(v, "bt.pair") == nil || findAction(v, "bt.connect") != nil ||
		table(v, "Bluetooth devices").Selectable {
		t.Fatalf("nothing paired: %+v", v.Actions)
	}
	// Sin los comandos de Omarchy o sin adaptador: ninguna bt.*.
	for _, f := range []func(*model.System){
		func(y *model.System) { y.BTTools = false },
		func(y *model.System) { y.BTPresent = false },
	} {
		s = devicesSnap("en")
		f(s.System)
		for _, a := range buildDevices(t, s).Actions {
			if strings.HasPrefix(a.ID, "bt.") {
				t.Errorf("unexpected %s", a.ID)
			}
		}
	}
	// El fixture (sin foco: sin audio; sin BlueZ): ninguna.
	if v := build(t, "devices", "threadripper", Options{}); len(v.Actions) != 0 {
		t.Fatalf("threadripper actions = %+v", v.Actions)
	}
}
