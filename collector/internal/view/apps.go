package view

import (
	"strconv"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/apps"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

func init() {
	builders["apps"] = appsView
	heroes["apps"] = appsHero
	actions["apps"] = appsActions
}

// appsView: una tabla de ventanas por app (con su consumo arriba), las 3
// apps sin ventana que más consumen y, siempre, la nota de la red TCP.
func appsView(s *model.Snapshot, o Options) []Block {
	l := o.Lang
	a := s.Apps
	if a == nil {
		return appendUnavailable([]Block{Text(i18n.T(l, "Reading the applications…"), "")}, s, "apps")
	}
	cols := []string{i18n.T(l, "Window"), i18n.T(l, "Workspace"), i18n.T(l, "Monitor"), i18n.T(l, "State")}
	var bs []Block
	for _, app := range a.List {
		if len(app.Windows) == 0 {
			continue
		}
		rows := make([]Row, 0, len(app.Windows))
		for _, w := range app.Windows {
			rows = append(rows, Row{ID: windowRowID(w), Cells: []string{windowTitle(w), w.Workspace, dash(w.Monitor), windowState(w, l)},
				Flags: rowFlags(app, false)})
		}
		bs = append(bs, Group(app.Name, Text(appLine(app, l), ""), Table("", cols, rows, true)))
	}
	if len(bs) == 0 {
		bs = append(bs, Text(i18n.T(l, "No open windows"), ""))
	}
	// Las 3 sin ventana que más usan y, después, las congeladas que no
	// entraron: una app congelada usa 0 % de CPU y sin esto no se la podría
	// reanudar ni cerrar desde acá.
	var rest []Row
	for _, app := range a.List {
		if len(app.Windows) == 0 && (len(rest) < 3 || app.Frozen) {
			name := app.Name
			if app.Frozen {
				name = "❄ " + name
			}
			rest = append(rest, Row{ID: "app:" + app.ID, Cells: []string{name, Pct(app.CPUPct), Bytes(app.MemB), diskCell(app), strconv.Itoa(app.Procs)},
				Flags: rowFlags(app, true)})
		}
	}
	if len(rest) > 0 {
		bs = append(bs, Table(i18n.T(l, "Without a window: the heaviest"),
			[]string{i18n.T(l, "App"), "CPU", i18n.T(l, "Memory"), i18n.T(l, "Disk"), i18n.T(l, "Processes")}, rest, true))
	}
	for _, p := range []string{"apps.windows", "apps.gpu", "apps.net", "apps.disk"} {
		bs = appendUnavailable(bs, s, p)
	}
	// La de TCP va última: el top de Red la reusa.
	return append(bs, Text(diskNote(l), ""), Text(tcpNote(l), ""))
}

// diskNote dice qué cuenta el disco por app (solo en Aplicaciones).
func diskNote(l string) string {
	return i18n.T(l, "Disk per app counts what reaches the disk; reads served from memory are not included.")
}

// diskCell es lo que la app lee y escribe en el disco por segundo, sumado,
// o "—" sin dato.
func diskCell(a model.App) string {
	if a.DiskReadBps == nil || a.DiskWriteBps == nil {
		return "—"
	}
	return Rate(*a.DiskReadBps + *a.DiskWriteBps)
}

func tcpNote(l string) string {
	return i18n.T(l, "Network per app counts TCP only: QUIC and UDP (video in the browser, calls) are not included.")
}

func rowFlags(a model.App, noWindow bool) []string {
	var f []string
	if a.Protected {
		f = append(f, "protected")
	}
	if noWindow {
		f = append(f, "nowindow")
	}
	return f
}

// windowRowID es "<dirección>:<stableId>": la acción comprueba que la
// dirección sigue siendo de esa ventana (Hyprland las reusa).
func windowRowID(w model.Window) string {
	if w.StableID == "" {
		return w.Address
	}
	return w.Address + ":" + w.StableID
}

func windowTitle(w model.Window) string {
	t := w.Title
	if t == "" {
		t = w.Class
	}
	if w.Focused {
		t = "● " + t
	}
	return t
}

func windowState(w model.Window, l string) string {
	var s []string
	if w.Floating {
		s = append(s, i18n.T(l, "floating"))
	}
	if w.Pinned {
		s = append(s, i18n.T(l, "pinned"))
	}
	switch w.Fullscreen {
	case 1:
		s = append(s, i18n.T(l, "maximized"))
	case 2, 3:
		s = append(s, i18n.T(l, "fullscreen"))
	}
	if w.XWayland {
		s = append(s, "XWayland")
	}
	if w.Grouped {
		s = append(s, i18n.T(l, "grouped"))
	}
	if w.InhibitIdle {
		s = append(s, i18n.T(l, "keeps screen on"))
	}
	return dash(strings.Join(s, ", "))
}

// appLine es el consumo de la app: CPU, memoria (aprox.), GPU, red y disco si se
// pudieron medir, y si está congelada.
func appLine(a model.App, l string) string {
	parts := []string{"CPU " + Pct(a.CPUPct), Bytes(a.MemB) + " " + i18n.T(l, "approx.")}
	if a.GPUPct != nil {
		parts = append(parts, "GPU "+Pct(*a.GPUPct))
	}
	if a.NetRxBps != nil && a.NetTxBps != nil {
		parts = append(parts, "↓ "+Rate(*a.NetRxBps)+" ↑ "+Rate(*a.NetTxBps))
	}
	if a.DiskReadBps != nil && a.DiskWriteBps != nil && (*a.DiskReadBps > 0 || *a.DiskWriteBps > 0) {
		parts = append(parts, i18n.F(l, "Disk R %s W %s", Rate(*a.DiskReadBps), Rate(*a.DiskWriteBps)))
	}
	if a.Frozen {
		parts = append([]string{"❄ " + i18n.T(l, "frozen")}, parts...)
	}
	return strings.Join(parts, " · ")
}

func appsHero(s *model.Snapshot, o Options) *Block {
	a := s.Apps
	if a == nil {
		return nil
	}
	l := o.Lang
	nApps, nWin := 0, 0
	for _, app := range a.List {
		if len(app.Windows) > 0 {
			nApps++
			nWin += len(app.Windows)
		}
	}
	stats := []Block{
		Stat(i18n.T(l, "Apps with windows"), strconv.Itoa(nApps), "", ""),
		Stat(i18n.T(l, "Windows"), strconv.Itoa(nWin), "", ""),
		statOf("CPU", Pct(a.TotalCPUPct), ""),
		statOf(i18n.T(l, "Memory"), Bytes(a.TotalMemB), ""),
	}
	var lines []string
	if n := len(a.Frozen); n > 0 {
		lines = append(lines, i18n.F(l, "%d frozen", n))
	}
	return heroOf(stats, lines, nil)
}

// appsActions son las acciones de apps.Specs. Mover a otro monitor necesita
// más de uno.
func appsActions(s *model.Snapshot, o Options) []Action {
	l := o.Lang
	var mons []string
	if s.Apps != nil {
		mons = s.Apps.Monitors
	}
	out := []Action{}
	for _, sp := range apps.Specs {
		if sp.Input == "monitor" && len(mons) < 2 {
			continue
		}
		a := Action{Key: sp.Key, Label: sp.Label(l), ID: sp.ID, Target: "row", Mode: "background"}
		if sp.Confirm != nil {
			a.Confirm = sp.Confirm(l)
		}
		switch sp.Input {
		case "workspace":
			a.Input = &Input{Type: "number", Prompt: i18n.T(l, "Workspace")}
		case "monitor":
			a.Input = &Input{Type: "choice", Prompt: i18n.T(l, "Monitor"), Options: mons, Default: mons[0]}
		case "size":
			a.Input = &Input{Type: "choice", Prompt: i18n.T(l, "Size"), Options: []string{"50%", "70%", "90%"}, Default: "70%"}
		}
		if sp.Window {
			a.Hide = append(a.Hide, "nowindow")
		}
		if sp.Guarded {
			a.Hide = append(a.Hide, "protected")
		}
		out = append(out, a)
	}
	return out
}
