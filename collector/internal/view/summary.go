package view

import (
	"fmt"
	"slices"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

// DefaultCards son las tarjetas del Resumen si el usuario no eligió otras.
var DefaultCards = []string{"cpu", "memory", "gpu", "storage", "network"}

func init() {
	builders["summary"] = summaryView
	warnings = Warnings
}

func summaryView(s *model.Snapshot, o Options) []Block {
	l := o.Lang
	var bs []Block
	if h := summaryHeader(s, o); h != nil {
		bs = append(bs, *h)
	}
	ids := o.Cards
	if len(ids) == 0 {
		ids = DefaultCards
	}
	var cs []Block
	for _, id := range ids {
		c := card(id, s, o)
		if len(c) == 0 {
			if u := Unavailable(s, cardSource(id)); u != nil {
				c = []Block{Stat(cardLabel(id, l), "—", "", u.Level, u.Text)}
			}
		}
		cs = append(cs, c...)
	}
	if len(cs) > 0 {
		bs = append(bs, Cards(cs...))
	}
	if w := Warnings(s, o); len(w) > 0 {
		bs = append(bs, Group(i18n.T(l, "Needs attention"), w...))
	} else {
		bs = append(bs, Text(i18n.T(l, "All clear"), ""))
	}
	return bs
}

// summaryHeader es el nombre del equipo en grande, lo que tiene adentro y
// hace cuánto está encendido.
func summaryHeader(s *model.Snapshot, o Options) *Block {
	if s.System == nil {
		return nil
	}
	l := o.Lang
	var gpus []string
	for _, g := range s.GPUs {
		gpus = append(gpus, g.Name)
	}
	var cpu, mem string
	if s.CPU != nil {
		cpu = s.CPU.Model
	}
	if s.Memory != nil {
		mem = Bytes(s.Memory.TotalB)
	}
	// The machine goes in the hero's lines, which have the width the
	// hostname's column does not.
	lines := []string{}
	for _, x := range []string{cpu, join(" · ", strings.Join(gpus, " + "), mem), i18n.F(l, "up %s", Duration(s.System.UptimeS, l))} {
		if x != "" {
			lines = append(lines, x)
		}
	}
	h := Hero([]Block{Stat("", s.System.Hostname, "", "")}, lines, nil)
	return &h
}

// cardSource es el prefijo de Unavailable que explica una tarjeta vacía.
func cardSource(id string) string {
	switch id {
	case "cputemp":
		return "cpu.temp"
	case "gputemp":
		return "gpu.temp"
	case "load":
		return "cpu"
	}
	return id
}

// cardLabel es el título de una tarjeta, para la que no se pudo leer.
func cardLabel(id, l string) string {
	switch id {
	case "cpu":
		return "CPU"
	case "gpu":
		return "GPU"
	}
	return i18n.T(l, map[string]string{
		"cputemp": "CPU temperature", "gputemp": "GPU temperature", "memory": "Memory",
		"storage": "Disk /", "network": "Network", "power": "Power", "load": "Load",
	}[id])
}

func card(id string, s *model.Snapshot, o Options) []Block {
	l := o.Lang
	switch id {
	case "cpu":
		if s.CPU == nil || s.CPU.UsagePct == nil {
			return nil
		}
		u := *s.CPU.UsagePct
		temp := ""
		if s.CPU.TempC != nil {
			temp = Temp(*s.CPU.TempC, o.Units)
		}
		st := statOf("CPU", Pct(u), Level(u, 70, 90), join(" · ", temp, mhz(s.CPU.AvgMHz)))
		return []Block{st.WithValues(o.recent("cpu.usage")).WithLink("cpu")}
	case "cputemp":
		if s.CPU == nil || s.CPU.TempC == nil {
			return nil
		}
		t := *s.CPU.TempC
		return []Block{statOf(i18n.T(l, "CPU temperature"), Temp(t, o.Units), Level(t, 80, 90)).WithValues(o.recent("cpu.temp")).WithLink("cpu")}
	case "memory":
		if s.Memory == nil {
			return nil
		}
		m := s.Memory
		st := statOf(i18n.T(l, "Memory"), Pct(m.UsedPct), Level(m.UsedPct, 80, 90), i18n.F(l, "%s of %s", Bytes(m.UsedB), Bytes(m.TotalB)))
		return []Block{st.WithValues(o.recent("mem.used_pct")).WithLink("memory")}
	case "gpu":
		var out []Block
		for _, g := range s.GPUs {
			if g.Suspended || g.UsagePct == nil {
				continue
			}
			temp := ""
			if g.TempC != nil {
				temp = Temp(*g.TempC, o.Units)
			}
			st := statOf(g.Name, Pct(*g.UsagePct), Level(*g.UsagePct, 80, 95), temp).WithLink("gpu")
			if len(out) == 0 { // el historial gpu.* es de la primera GPU despierta
				st = st.WithValues(o.recent("gpu.usage"))
			}
			out = append(out, st)
		}
		return out
	case "gputemp":
		for _, g := range s.GPUs {
			if !g.Suspended && g.TempC != nil {
				return []Block{statOf(i18n.T(l, "GPU temperature"), Temp(*g.TempC, o.Units), Level(*g.TempC, 80, 90)).WithValues(o.recent("gpu.temp")).WithLink("gpu")}
			}
		}
	case "storage":
		if s.Storage == nil {
			return nil
		}
		for _, f := range s.Storage.Filesystems {
			if slices.Contains(f.Mounts, "/") {
				st := statOf(i18n.T(l, "Disk /"), Bytes(f.FreeB), Level(f.UsedPct, 85, 95), i18n.F(l, "free of %s", Bytes(f.SizeB)))
				return []Block{st.WithValue(f.UsedPct / 100).WithLink("storage")}
			}
		}
	case "network":
		if s.Network == nil {
			return nil
		}
		st := statOf(i18n.T(l, "Network"), Rate(s.Network.RxBps), "", "↑ "+Rate(s.Network.TxBps))
		return []Block{st.WithValues(o.recent("net.rx")).WithLink("network")}
	case "power":
		if s.Power == nil {
			return nil
		}
		for _, b := range s.Power.Batteries {
			if !b.Peripheral {
				st := statOf(i18n.T(l, "Battery"), Pct(b.Pct), Level(100-b.Pct, 80, 90), batteryStatus(b.Status, l))
				return []Block{st.WithValue(b.Pct / 100).WithLink("power")}
			}
		}
		var w float64
		found := false
		for _, d := range s.Power.RAPL {
			if strings.HasPrefix(d.Name, "package-") {
				w += d.Watts
				found = true
			}
		}
		if found {
			return []Block{statOf(i18n.T(l, "Power"), Watts(w), "", i18n.T(l, "CPU package")).WithLink("power")}
		}
	case "load":
		if s.CPU == nil {
			return nil
		}
		ld := s.CPU.Load
		st := Stat(i18n.T(l, "Load"), fmt.Sprintf("%.2f", ld[0]), "", "", i18n.F(l, "%s (5 and 15 min)", fmt.Sprintf("%.2f · %.2f", ld[1], ld[2])))
		return []Block{st.WithLink("cpu")}
	}
	return nil
}

// Warnings es lo que requiere atención, cada aviso con un link a su sección.
// Un disco da un solo aviso, con todos sus motivos.
func Warnings(s *model.Snapshot, o Options) []Block {
	l := o.Lang
	var out []Block
	if s.System != nil {
		for _, u := range s.System.FailedUnits {
			out = append(out, Link(i18n.F(l, "Failed service: %s (%s)", u.Name, scopeName(u.Scope, l)), "warn", "about"))
		}
	}
	if s.Storage != nil {
		for _, d := range s.Storage.Disks {
			m := d.Smart
			if m == nil {
				continue
			}
			if m.Failing {
				out = append(out, Link(i18n.F(l, "Disk failing: %s", d.Model), "crit", "storage"))
				continue
			}
			if !diskAttention(m) {
				continue
			}
			why := diskReasons(m, l)
			out = append(out, Link(i18n.F(l, "Disk warning: %s: %s", d.Model, strings.Join(why, ", ")), "warn", "storage"))
		}
	}
	if s.Firmware != nil {
		for _, d := range s.Firmware.Devices {
			if d.Available != "" {
				out = append(out, Link(i18n.F(l, "Firmware update available: %s", d.Name), "warn", "about"))
			}
		}
	}
	if w := clockWarning(s, l); w != "" {
		out = append(out, Link(w, "warn", "about"))
	}
	for _, a := range attention {
		out = append(out, a.warn(s, l)...)
	}
	return out
}

// clockWarning avisa si la hora no se sincroniza sola (hay NTP para activar
// y está apagado) o si, con NTP, el reloj sigue sin sincronizar pasados 10
// minutos del arranque. "" si no hay nada que avisar o no hay datos del reloj.
func clockWarning(s *model.Snapshot, l string) string {
	if s.System == nil || s.System.Time == nil {
		return ""
	}
	t := s.System.Time
	switch {
	case t.CanNTP && !t.NTP:
		return i18n.T(l, "The time is not synchronized automatically")
	case t.NTP && !t.Synced && s.System.UptimeS > 600:
		return i18n.T(l, "The clock is not synchronized")
	}
	return ""
}

func scopeName(scope, l string) string {
	switch scope {
	case "system":
		return i18n.T(l, "system")
	case "user":
		return i18n.T(l, "user")
	}
	return scope
}

func batteryStatus(st, l string) string {
	switch st {
	case "Charging":
		return i18n.T(l, "Charging")
	case "Discharging":
		return i18n.T(l, "Discharging")
	case "Full":
		return i18n.T(l, "Full")
	case "Not charging":
		return i18n.T(l, "Not charging")
	case "Unknown":
		return i18n.T(l, "Unknown")
	}
	return st
}
