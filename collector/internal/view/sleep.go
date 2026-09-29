package view

import (
	"slices"
	"strconv"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

// sleepBlocks es lo de Energía que dice qué impide suspender o apagar la
// pantalla (solo con la sección abierta: Inhibit nil = no se miró) y cómo
// suspende y qué puede despertar el equipo (con lo lento). allRead es false
// si logind o Hyprland no se pudieron leer: entonces no se dice "nada lo
// impide", porque no se sabe.
func sleepBlocks(p *model.Power, l string, allRead bool) []Block {
	var bs []Block
	if in := p.Inhibit; in != nil {
		if in.StayAwake {
			bs = append(bs, Text(i18n.T(l, "Stay awake is on (Omarchy): the screen does not lock or turn off by itself"), ""))
		}
		for _, w := range in.Windows {
			bs = append(bs, Text(i18n.F(l, "Keeps the screen on: %s", join(" — ", w.Class, w.Title)), ""))
		}
		var rs []Row
		var delay []string
		for i, x := range in.Logind {
			if x.Mode == "delay" {
				if x.Who != "" && !slices.Contains(delay, x.Who) {
					delay = append(delay, x.Who)
				}
				continue
			}
			what := make([]string, 0, len(x.What))
			for _, w := range x.What {
				what = append(what, inhibitWhat(w, l))
			}
			rs = append(rs, Row{ID: "inhibitor:" + strconv.Itoa(i), Cells: []string{dash(x.Who), strings.Join(what, ", "), dash(x.Why)}})
		}
		if len(rs) > 0 {
			bs = append(bs, Table(i18n.T(l, "Preventing sleep or idle"),
				[]string{i18n.T(l, "Who"), i18n.T(l, "What"), i18n.T(l, "Why")}, rs, false))
		}
		if len(delay) > 0 {
			bs = append(bs, Text(i18n.F(l, "Delaying sleep briefly (normal): %s", strings.Join(delay, ", ")), ""))
		}
		if allRead && len(rs) == 0 && len(in.Windows) == 0 && !in.StayAwake {
			bs = append(bs, Text(i18n.T(l, "Nothing is keeping the computer or the screen awake"), "ok"))
		}
	}
	mode := sleepModeName(p.SleepMode, l)
	var others []string
	for _, m := range p.SleepModes {
		if m != p.SleepMode {
			others = append(others, m)
		}
	}
	if mode != "" && len(others) > 0 {
		mode = i18n.F(l, "%s (also: %s)", mode, strings.Join(others, ", "))
	}
	var wake []string
	for _, w := range p.Wakeup {
		if w.Kind == "net" {
			wake = append(wake, i18n.F(l, "%s (if Wake-on-LAN is on)", w.Name))
			continue
		}
		wake = append(wake, w.Name)
	}
	return append(bs, kvIf(i18n.T(l, "Sleep"),
		[2]string{i18n.T(l, "Suspend mode"), mode},
		[2]string{i18n.T(l, "Can wake the computer"), strings.Join(wake, ", ")})...)
}

// inhibitWhat nombra lo que frena un inhibidor de logind; uno desconocido
// queda tal cual.
func inhibitWhat(w, l string) string {
	switch w {
	case "shutdown":
		return i18n.T(l, "shut down")
	case "sleep":
		return i18n.T(l, "sleep")
	case "idle":
		return i18n.T(l, "screen off when idle")
	case "handle-power-key":
		return i18n.T(l, "power key")
	case "handle-suspend-key":
		return i18n.T(l, "suspend key")
	case "handle-hibernate-key":
		return i18n.T(l, "hibernate key")
	case "handle-reboot-key":
		return i18n.T(l, "reboot key")
	case "handle-lid-switch":
		return i18n.T(l, "lid switch")
	}
	return w
}

// sleepModeName nombra el modo de /sys/power/mem_sleep; uno desconocido
// queda tal cual.
func sleepModeName(m, l string) string {
	switch m {
	case "deep":
		return i18n.T(l, "Suspend to RAM (S3)")
	case "s2idle":
		return i18n.T(l, "Modern standby (s2idle)")
	case "shallow":
		return i18n.T(l, "Standby (S1)")
	}
	return m
}
