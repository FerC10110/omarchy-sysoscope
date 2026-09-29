package view

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

// hzText: una frecuencia con hasta dos decimales ("49.99", "60", "143.91").
func hzText(f float64) string { return strconv.FormatFloat(math.Round(f*100)/100, 'f', -1, 64) }

// resText: "3440×1440".
func resText(v *model.MonitorMode) string { return fmt.Sprintf("%d×%d", v.Width, v.Height) }

// activeLive es el modo en uso de un monitor que muestra su propia imagen
// (conectado, encendido, sin espejar a otro y con un modo que se entiende),
// o nil.
func activeLive(m model.Monitor) *model.MonitorMode {
	v := m.Live
	if !m.Connected || v == nil || v.Disabled || v.MirrorOf != "" || v.Width <= 0 || v.Height <= 0 || v.RefreshHz <= 0 {
		return nil
	}
	return v
}

// monitorWarnings: un monitor que va por debajo de la mejor frecuencia que
// ofrece a la resolución en uso, por 1 Hz o más (así 59.94 contra 60 no
// avisa). Se lee con lo lento: vale para el Resumen.
func monitorWarnings(y *model.System, l string) []string {
	if y == nil {
		return nil
	}
	var out []string
	for _, m := range y.Monitors {
		if v := activeLive(m); v != nil && v.BestHz-v.RefreshHz >= 1 {
			out = append(out, i18n.F(l, "%s runs at %s Hz; it offers %s Hz at %s", m.Connector, hzText(v.RefreshHz), hzText(v.BestHz), resText(v)))
		}
	}
	return out
}

// hdmiNotes: por HDMI, un monitor que declara en su EDID bastante más de lo
// que ofrece a esta resolución (por 5 Hz o más). Informativo: ese máximo
// vale para otras resoluciones y otra entrada.
func hdmiNotes(y *model.System, l string) []string {
	if y == nil {
		return nil
	}
	var out []string
	for _, m := range y.Monitors {
		v := activeLive(m)
		if v == nil || !strings.HasPrefix(m.Connector, "HDMI") || m.MaxVHz == 0 {
			continue
		}
		best := max(v.BestHz, v.RefreshHz)
		if float64(m.MaxVHz)-best >= 5 {
			out = append(out, i18n.F(l, "%s: the monitor declares up to %d Hz, but over HDMI it offers %s Hz at %s. Another input (DisplayPort) or a lower resolution may allow more.",
				m.Connector, m.MaxVHz, hzText(best), resText(v)))
		}
	}
	return out
}

// usbVersion3: bcdUSB 3.00 o más.
func usbVersion3(v string) bool {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	return err == nil && f >= 3
}

func usbLabel(d model.USBDevice) string {
	if n := join(" ", d.Vendor, d.Product); n != "" {
		return n
	}
	return d.Path
}

// usbWarnings: un dispositivo que se anuncia USB 3 y anda a 480 Mb/s o
// menos. Es lo único seguro: un USB 3 en un puerto o cable USB 2 casi
// siempre se anuncia 2.10 (ver usbNotes).
func usbWarnings(y *model.System, l string) []string {
	if y == nil {
		return nil
	}
	var out []string
	for _, d := range y.USB {
		if usbVersion3(d.Version) && d.SpeedMbps > 0 && d.SpeedMbps <= 480 {
			out = append(out, i18n.F(l, "%s is a USB 3 device running at USB 2 speed", usbLabel(d)))
		}
	}
	return out
}

// usbNotes: almacenamiento o video a USB 2 en un puerto que tiene par
// SuperSpeed. Sin el BOS descriptor no se sabe si el dispositivo es USB 3:
// nota, no aviso.
func usbNotes(y *model.System, l string) []string {
	if y == nil {
		return nil
	}
	var out []string
	for _, d := range y.USB {
		if (d.Class == "08" || d.Class == "0e") && d.SSPort && d.SpeedMbps > 0 && d.SpeedMbps <= 480 && !usbVersion3(d.Version) {
			out = append(out, i18n.F(l, "%s: USB 2 speed on a USB 3 port (if the device is USB 3, check the cable)", usbLabel(d)))
		}
	}
	return out
}

// devicesWarnings son los avisos de Dispositivos para el Resumen y su badge
// (entrada de attention).
func devicesWarnings(s *model.Snapshot, l string) []Block {
	var out []Block
	for _, w := range append(monitorWarnings(s.System, l), usbWarnings(s.System, l)...) {
		out = append(out, Link(w, "warn", "devices"))
	}
	return out
}

// usbBlocks: los avisos, la tabla USB (con la versión) y las notas.
func usbBlocks(y *model.System, l string) []Block {
	var bs []Block
	for _, w := range usbWarnings(y, l) {
		bs = append(bs, Text(w, "warn"))
	}
	var rs []Row
	for _, d := range y.USB {
		speed := "—"
		if d.SpeedMbps > 0 {
			speed = strconv.FormatFloat(d.SpeedMbps, 'f', -1, 64) + " Mb/s"
		}
		rs = append(rs, Row{ID: d.Path, Cells: []string{d.Path, dash(join(" ", d.Vendor, d.Product)), dash(d.Version), speed, dash(d.Driver)}})
	}
	bs = append(bs, Table("USB", []string{i18n.T(l, "Port"), i18n.T(l, "Device"), "USB", i18n.T(l, "Speed"), i18n.T(l, "Driver")}, rs, false))
	for _, n := range usbNotes(y, l) {
		bs = append(bs, Text(n, ""))
	}
	return bs
}

// monitorBlocks: los avisos, la tabla de monitores y las notas de HDMI. Con
// el modo en uso de Hyprland en alguno, la tabla lo muestra; sin él, la de
// siempre con el modo preferido del EDID.
func monitorBlocks(y *model.System, l string) []Block {
	live := false
	for _, m := range y.Monitors {
		if m.Connected && m.Live != nil {
			live = true
		}
	}
	var rs []Row
	for _, m := range y.Monitors {
		if !m.Connected {
			continue
		}
		size := "—"
		if m.WidthMM > 0 && m.HeightMM > 0 {
			size = fmt.Sprintf("%d × %d cm", (m.WidthMM+5)/10, (m.HeightMM+5)/10)
		}
		// Manufacturer es el código PNP ("DEL"); el modelo del EDID ya trae la marca.
		name := m.Model
		if name == "" {
			name = m.Manufacturer
		}
		if !live {
			rs = append(rs, Row{ID: m.Connector, Cells: []string{dash(m.Connector), dash(name), size, dash(m.Preferred)}})
			continue
		}
		inUse, best, vrr, color := "—", "—", "—", "—"
		if v := m.Live; v != nil {
			switch {
			case v.Disabled:
				inUse = i18n.T(l, "Off")
			case v.MirrorOf != "":
				inUse = i18n.F(l, "Mirrors %s", v.MirrorOf)
			case v.Width > 0 && v.Height > 0 && v.RefreshHz > 0:
				inUse = fmt.Sprintf("%s @ %s Hz", resText(v), hzText(v.RefreshHz))
				if v.Scale > 0 && v.Scale != 1 {
					inUse += " ×" + hzText(v.Scale)
				}
			}
			if v.BestHz > 0 {
				best = hzText(v.BestHz) + " Hz"
			}
			vrr = i18n.T(l, "off")
			if v.VRR {
				vrr = i18n.T(l, "on")
			}
			color = dash(join(" · ", v.Preset, v.Format))
		}
		rs = append(rs, Row{ID: m.Connector, Cells: []string{dash(m.Connector), dash(name), size, inUse, best, vrr, color}})
	}
	if len(rs) == 0 {
		return nil
	}
	var bs []Block
	for _, w := range monitorWarnings(y, l) {
		bs = append(bs, Text(w, "warn"))
	}
	cols := []string{i18n.T(l, "Connector"), i18n.T(l, "Model"), i18n.T(l, "Size"), i18n.T(l, "Mode")}
	if live {
		cols = []string{i18n.T(l, "Connector"), i18n.T(l, "Model"), i18n.T(l, "Size"), i18n.T(l, "In use"),
			i18n.T(l, "Max at this resolution"), "VRR", i18n.T(l, "Color")}
	}
	bs = append(bs, Table(i18n.T(l, "Monitors"), cols, rs, false))
	for _, n := range hdmiNotes(y, l) {
		bs = append(bs, Text(n, ""))
	}
	return bs
}
