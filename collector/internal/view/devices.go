package view

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/wifi"
)

func init() {
	actions["devices"] = devicesActions
}

// nodeFlag es la bandera que identifica una fila de audio: cada v se
// esconde en las filas de los demás nodos.
func nodeFlag(name string) string { return "node:" + name }

// soundBlocks: las tablas Salidas y Entradas (nombre, volumen, silencio y
// la marca de la predeterminada). Sin la lectura de pactl, nada.
func soundBlocks(snd *model.Sound, l string) []Block {
	if snd == nil {
		return nil
	}
	cols := []string{i18n.T(l, "Device"), i18n.T(l, "Volume"), i18n.T(l, "Muted"), i18n.T(l, "Default")}
	var bs []Block
	for _, t := range []struct {
		title, kind string
		nodes       []model.AudioNode
	}{
		{i18n.T(l, "Outputs"), "output", snd.Outputs},
		{i18n.T(l, "Inputs"), "input", snd.Inputs},
	} {
		if len(t.nodes) == 0 {
			continue
		}
		rows := make([]Row, 0, len(t.nodes))
		for _, n := range t.nodes {
			def, flags := "", []string{"audio", t.kind, nodeFlag(n.Name)}
			if n.Default {
				def, flags = "✓", append(flags, "default")
			}
			rows = append(rows, Row{ID: n.Name, Cells: []string{wifi.Printable(n.Label), fmt.Sprintf("%d%%", n.Volume), yesNo(n.Muted, l), def},
				Flags: flags})
		}
		bs = append(bs, Table(t.title, cols, rows, true))
	}
	return bs
}

// btBlocks: el estado del adaptador y la tabla de dispositivos (conectados,
// emparejados y, al final, los cercanos que BlueZ recuerda de una
// búsqueda). Sin BlueZ, el motivo, como antes.
func btBlocks(s *model.Snapshot, l string) []Block {
	y := s.System
	if u := Unavailable(s, "system.bluetooth"); u != nil && !y.BTPresent && len(y.Bluetooth) == 0 {
		return []Block{*u}
	}
	state := i18n.T(l, "No adapter")
	switch {
	case y.BTPowered:
		state = i18n.T(l, "Powered on")
	case y.BTPresent:
		state = i18n.T(l, "Powered off")
	}
	bs := []Block{KV("Bluetooth", [2]string{i18n.T(l, "State"), state})}
	if len(y.Bluetooth) == 0 {
		return bs
	}
	rows := make([]Row, 0, len(y.Bluetooth))
	for _, d := range y.Bluetooth {
		conn, pair := "disconnected", "nearby"
		if d.Connected {
			conn = "connected"
		}
		if d.Paired {
			pair = "paired"
		}
		st := i18n.T(l, "Nearby")
		switch {
		case d.Connected:
			st = i18n.T(l, "Connected")
		case d.Paired:
			st = i18n.T(l, "Paired")
		}
		bat := "—"
		if d.Battery != nil {
			bat = fmt.Sprintf("%d%%", *d.Battery)
		}
		rows = append(rows, Row{ID: d.Address, Cells: []string{dash(wifi.Printable(d.Name)), st, bat}, Flags: []string{"bt", conn, pair}})
	}
	cols := []string{i18n.T(l, "Device"), i18n.T(l, "State"), i18n.T(l, "Battery")}
	return append(bs, Table(i18n.T(l, "Bluetooth devices"), cols, rows, btRowActions(y)))
}

// btRowActions: hay acciones de fila de Bluetooth con adaptador, los
// comandos de Omarchy y algún dispositivo emparejado o conectado.
func btRowActions(y *model.System) bool {
	if !y.BTPresent || !y.BTTools {
		return false
	}
	for _, d := range y.Bluetooth {
		if d.Paired || d.Connected {
			return true
		}
	}
	return false
}

// devicesActions: en las salidas y entradas, predeterminada (d), silencio
// (m), volumen ±5 (+ -) y exacto (v); en Bluetooth, prender o apagar (b) y
// emparejar en el panel de Omarchy (e) y, en un emparejado, conectar (c),
// desconectar (x) y olvidar (f, con confirmación). Las de fila se esconden
// en las filas del otro tipo (banderas audio y bt).
func devicesActions(s *model.Snapshot, o Options) []Action {
	l := o.Lang
	out := []Action{}
	y := s.System
	if y == nil {
		return out
	}
	if snd := y.Sound; snd != nil && len(snd.Outputs)+len(snd.Inputs) > 0 {
		out = append(out,
			Action{Key: "d", Label: i18n.T(l, "Make default"), ID: "audio.default", Target: "row", Mode: "background", Hide: []string{"bt", "default"}},
			Action{Key: "m", Label: i18n.T(l, "Mute / unmute"), ID: "audio.mute", Target: "row", Mode: "background", Hide: []string{"bt"}},
			Action{Key: "+", Label: i18n.T(l, "Volume up"), ID: "audio.up", Target: "row", Mode: "background", Hide: []string{"bt"}},
			Action{Key: "-", Label: i18n.T(l, "Volume down"), ID: "audio.down", Target: "row", Mode: "background", Hide: []string{"bt"}},
		)
		out = append(out, volumeActions(snd, l)...)
	}
	if !y.BTPresent || !y.BTTools {
		return out
	}
	power := i18n.T(l, "Turn Bluetooth on")
	if y.BTPowered {
		power = i18n.T(l, "Turn Bluetooth off")
	}
	out = append(out,
		Action{Key: "b", Label: power, ID: "bt.power", Target: "section", Mode: "background"},
		Action{Key: "e", Label: i18n.T(l, "Pair a device"), ID: "bt.pair", Target: "section", Mode: "background"})
	if !btRowActions(y) {
		return out
	}
	forget := i18n.T(l, "Forget this device? It has to be paired again to use it.")
	if !y.BTPowered {
		// omarchy-bluetooth-device forget prende el adaptador para olvidar.
		forget = i18n.T(l, "Forget this device? Bluetooth is turned on to do it.")
	}
	return append(out,
		Action{Key: "c", Label: i18n.T(l, "Connect"), ID: "bt.connect", Target: "row", Mode: "background", Hide: []string{"audio", "connected", "nearby"}},
		Action{Key: "x", Label: i18n.T(l, "Disconnect"), ID: "bt.disconnect", Target: "row", Mode: "background", Hide: []string{"audio", "disconnected"}},
		Action{Key: "f", Label: i18n.T(l, "Forget"), ID: "bt.forget", Target: "row", Mode: "background", Hide: []string{"audio", "nearby"},
			Confirm: forget})
}

// volumeActions: una v por fila de audio, cada una con el volumen de su
// fila (tope 100) como dato por defecto y escondida en las demás filas (su
// bandera node:<nombre>) y en las de Bluetooth.
func volumeActions(snd *model.Sound, l string) []Action {
	nodes := append(slices.Clone(snd.Outputs), snd.Inputs...)
	out := make([]Action, 0, len(nodes))
	for _, n := range nodes {
		hide := []string{"bt"}
		for _, m := range nodes {
			if m.Name != n.Name {
				hide = append(hide, nodeFlag(m.Name))
			}
		}
		out = append(out, Action{Key: "v", Label: i18n.T(l, "Set volume"), ID: "audio.volume", Target: "row", Mode: "background", Hide: hide,
			Input: &Input{Type: "number", Prompt: i18n.T(l, "Volume (0–100)"), Default: strconv.Itoa(min(n.Volume, 100))}})
	}
	return out
}
