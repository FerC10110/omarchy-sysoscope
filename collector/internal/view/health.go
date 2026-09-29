package view

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

const (
	errorSourcesShown = 3 // fuentes con más errores en el kv
	oomNamesShown     = 5 // nombres de procesos matados en la fila de Memoria
)

// errorsCount es la cantidad de errores del journal: "1000+" en el tope y
// "—" si todavía no se leyó.
func errorsCount(h *model.Health) string {
	switch {
	case h == nil || h.Errors == nil:
		return "—"
	case h.Errors.Capped:
		return strconv.Itoa(h.Errors.Count) + "+"
	}
	return strconv.Itoa(h.Errors.Count)
}

// stamp es la fecha local de una fila; "—" si no se sabe.
func stamp(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Local().Format("2006-01-02 15:04")
}

// hasUnavailable dice si hay un faltante con ese What exacto.
func hasUnavailable(s *model.Snapshot, what string) bool {
	for _, u := range s.Unavailable {
		if u.What == what {
			return true
		}
	}
	return false
}

// healthBlocks es el grupo Salud de Mantenimiento: sin avisos (los errores
// y los crashes se leen solo con la sección abierta, spec §2.4).
//
// El motor puede publicar Health no nil con Errors y Crashes en nil y sin
// ningún unavailable maintenance.health.{errors,crashes}: pasó justo
// después de que se abrió Memoria (solo se leyeron los OOM) mientras la
// lectura completa de Mantenimiento todavía no corrió. Se trata igual que
// Health == nil para esa parte ("Reading the journal…"), sin tocar lo de
// OOM que sí se leyó. Si hay un unavailable de por medio, gana el motivo.
func healthBlocks(s *model.Snapshot, l string) []Block {
	h := s.Maintenance.Health
	if h == nil {
		in := healthUnavailable(nil, s, l)
		if len(in) == 0 {
			in = append(in, Text(i18n.T(l, "Reading the journal…"), ""))
		}
		return []Block{Group(i18n.T(l, "Health"), in...)}
	}
	errs := ""
	if e := h.Errors; e != nil {
		errs = errorsCount(h)
		var top []string
		for _, src := range e.Sources[:min(len(e.Sources), errorSourcesShown)] {
			top = append(top, src.Name+" "+strconv.Itoa(src.Count))
		}
		if len(top) > 0 {
			errs += " (" + strings.Join(top, ", ") + ")"
		}
	}
	// Journal parcial: las muertes de systemd-oomd están en el journal del
	// sistema, que no se ve; su 0 no se afirma.
	partial := (h.Errors != nil && h.Errors.Partial) || (h.OOM != nil && h.OOM.Partial)
	oom := ""
	if h.OOM != nil {
		var kernel uint64
		if s.Memory != nil && s.Memory.OOMKills != nil {
			kernel = *s.Memory.OOMKills
		}
		if partial {
			oom = i18n.F(l, "kernel %d · systemd-oomd not visible", kernel)
		} else {
			oom = i18n.F(l, "kernel %d · systemd-oomd %d", kernel, h.OOM.Oomd)
		}
	}
	in := kvIf("", [2]string{i18n.T(l, "Errors this boot"), errs}, [2]string{i18n.T(l, "Killed for lack of memory (since boot)"), oom})
	if partial {
		in = append(in, Text(i18n.T(l, "Only your own messages (join the wheel group to see the system's)"), ""))
	}
	switch {
	case h.Errors != nil:
		e := h.Errors
		if len(e.Recent) > 0 {
			rs := make([]Row, 0, len(e.Recent))
			for i, x := range e.Recent {
				rs = append(rs, Row{ID: strconv.Itoa(i), Cells: []string{stamp(x.Time), x.Source, dash(x.Message)}})
			}
			in = append(in, Table(i18n.T(l, "Recent errors"),
				[]string{i18n.T(l, "Time"), i18n.T(l, "Source"), i18n.T(l, "Message")}, rs, false))
		}
	case h.Crashes == nil && !hasUnavailable(s, "maintenance.health.errors") && !hasUnavailable(s, "maintenance.health.crashes"):
		// Ni errores ni crashes se leyeron todavía (Memoria abrió antes que
		// Mantenimiento hiciera su primera lectura completa) y nada explica
		// por qué: es "sin leer", no "cero" ni una falla.
		in = append(in, Text(i18n.T(l, "Reading the journal…"), ""))
	}
	if c := h.Crashes; c != nil {
		if len(c.List) == 0 {
			in = append(in, Text(i18n.T(l, "No crashes in the last 7 days"), "ok"))
		} else {
			rs := make([]Row, 0, len(c.List))
			for _, x := range c.List {
				sig := x.SignalName
				if sig == "" {
					sig = strconv.Itoa(x.Signal)
				}
				pid := strconv.Itoa(x.PID)
				rs = append(rs, Row{ID: pid, Cells: []string{stamp(x.Time), pid, x.Program, sig, x.User}})
			}
			in = append(in, Table(i18n.F(l, "Crashes in the last 7 days (%d)", c.Total),
				[]string{i18n.T(l, "Date"), "PID", i18n.T(l, "Program"), i18n.T(l, "Signal"), i18n.T(l, "User")}, rs, false),
				Text(i18n.T(l, "To see why one crashed: coredumpctl info <PID>"), ""))
		}
	}
	if o := h.OOM; o != nil && len(o.List) > 0 {
		rs := make([]Row, 0, len(o.List))
		for i, k := range o.List {
			rs = append(rs, Row{ID: strconv.Itoa(i), Cells: []string{stamp(k.Time), k.Program, k.By}})
		}
		in = append(in, Table(i18n.T(l, "Killed for lack of memory"),
			[]string{i18n.T(l, "Date"), i18n.T(l, "Program"), i18n.T(l, "By")}, rs, false))
	}
	in = healthUnavailable(in, s, l)
	return []Block{Group(i18n.T(l, "Health"), in...)}
}

// healthPart es el texto de una parte de la salud que no se pudo leer, con
// la parte delante; "" si what no es una de las tres.
func healthPart(l, what, msg string) string {
	switch what {
	case "maintenance.health.errors":
		return i18n.F(l, "Errors this boot: not available (%s)", msg)
	case "maintenance.health.crashes":
		return i18n.F(l, "Crashes: not available (%s)", msg)
	case "maintenance.health.oom":
		return i18n.F(l, "Killed for lack of memory: not available (%s)", msg)
	}
	return ""
}

// healthUnavailable agrega un texto por cada parte de la salud que no se
// pudo leer, con la parte delante (pueden fallar varias a la vez, cada una
// con su motivo). Otro faltante bajo maintenance.health sale como siempre.
func healthUnavailable(bs []Block, s *model.Snapshot, l string) []Block {
	known := false
	for _, u := range s.Unavailable {
		if text := healthPart(l, u.What, u.Message); text != "" {
			bs = append(bs, Text(text, "warn"))
			known = true
		}
	}
	if !known {
		bs = appendUnavailable(bs, s, "maintenance.health")
	}
	return bs
}

// memoryWarnings es el aviso de Memoria para el Resumen y su badge (entrada
// de attention): solo la cuenta del kernel, que se lee en cada muestra.
func memoryWarnings(s *model.Snapshot, l string) []Block {
	if s.Memory == nil || s.Memory.OOMKills == nil || *s.Memory.OOMKills == 0 {
		return nil
	}
	n := *s.Memory.OOMKills
	text := i18n.F(l, "%d processes were killed for lack of memory since boot", n)
	if n == 1 {
		text = i18n.T(l, "1 process was killed for lack of memory since boot")
	}
	return []Block{Link(text, "warn", "memory")}
}

// oomRow es la fila de Memoria: la cuenta del kernel, sus nombres si ya se
// leyeron y los de systemd-oomd si hubo.
func oomRow(s *model.Snapshot, l string) string {
	out := strconv.FormatUint(*s.Memory.OOMKills, 10)
	if s.Maintenance == nil || s.Maintenance.Health == nil || s.Maintenance.Health.OOM == nil {
		return out
	}
	o := s.Maintenance.Health.OOM
	if names := killNames(o.List, "kernel"); names != "" {
		out += " (" + names + ")"
	}
	if o.Partial {
		// Journal parcial: lo de systemd-oomd no se ve (no es un cero).
		return out + " · " + i18n.T(l, "systemd-oomd not visible")
	}
	if o.Oomd > 0 {
		out += " · " + i18n.F(l, "%d by systemd-oomd", o.Oomd)
		if names := killNames(o.List, "systemd-oomd"); names != "" {
			out += " (" + names + ")"
		}
	}
	return out
}

// killNames son los programas distintos que mató by, el más reciente
// primero, hasta oomNamesShown.
func killNames(ks []model.OOMKill, by string) string {
	var names []string
	for _, k := range ks {
		if k.By == by && k.Program != "" && !slices.Contains(names, k.Program) && len(names) < oomNamesShown {
			names = append(names, k.Program)
		}
	}
	return strings.Join(names, ", ")
}
