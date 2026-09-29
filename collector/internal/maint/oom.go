package maint

import (
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/apps"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// KernelOOMArgs busca en el journal del kernel las muertes por falta de
// memoria de este arranque (~49 ms: solo si oom_kill > 0). El patrón cubre
// "Out of memory: Killed process" y "Memory cgroup out of memory: Killed
// process", no el "Out of memory [NV_ERR_NO_MEMORY]" del driver NVIDIA.
var KernelOOMArgs = []string{"-b", "-k", "-q", "--no-pager", "-o", "json", "-n", "20",
	"--output-fields=MESSAGE,__REALTIME_TIMESTAMP", "-g", "of memory: Killed process"}

// OomdArgs lee las muertes que escribió systemd-oomd en este arranque
// (~8 ms); no suman a oom_kill (mata desde espacio de usuario). -g '^Killed '
// hace que las 50 sean muertes y no otros mensajes suyos ("Considered…");
// en Go se vuelve a mirar que empiecen con "Killed ".
var OomdArgs = []string{"-b", "-q", "--no-pager", "-o", "json", "-n", "50",
	"--output-fields=MESSAGE,__REALTIME_TIMESTAMP", "-g", "^Killed ", "SYSLOG_IDENTIFIER=systemd-oomd"}

var kernelKill = regexp.MustCompile(`of memory: Killed process (\d+) \((.*?)\)(?:[ ,]|$)`)

// killLine es una muerte leída de un mensaje: quién (kernel o
// systemd-oomd), el pid y el programa (kernel) o la unidad (oomd).
type killLine struct{ by, pid, program, unit string }

// parseKill entiende "… of memory: Killed process <pid> (<comm>) …" del
// kernel y "Killed <cgroup> due to …" de systemd-oomd.
func parseKill(msg string) (killLine, bool) {
	if m := kernelKill.FindStringSubmatch(msg); m != nil {
		return killLine{by: "kernel", pid: m[1], program: m[2]}, true
	}
	rest, ok := strings.CutPrefix(msg, "Killed ")
	if !ok {
		return killLine{}, false
	}
	f := strings.Fields(rest)
	if len(f) == 0 {
		return killLine{}, false
	}
	base := path.Base(f[0])
	name := apps.UnitName(base)
	if name == "" {
		name = base
	}
	return killLine{by: "systemd-oomd", program: name, unit: f[0]}, true
}

// OOMMessage reduce el mensaje de una muerte a lo que se lee ("Out of
// memory: Killed process <pid> (<programa>)" o "Killed <unidad>"): así lo
// guarda una captura anonimizada. false si no es una muerte.
func OOMMessage(msg string) (string, bool) {
	k, ok := parseKill(msg)
	switch {
	case !ok:
		return "", false
	case k.by == "kernel":
		return "Out of memory: Killed process " + k.pid + " (" + k.program + ")", true
	}
	return "Killed " + k.unit, true
}

// readKills corre journalctl con args y deja las muertes de by. false si
// falló (con el unavailable maintenance.health.oom).
func readKills(env source.Env, args []string, by string, r *collect.Report) ([]model.OOMKill, bool) {
	out, err := runHealth(env, "journalctl", args)
	if err != nil && !NoEntries(out, err) {
		healthErr(r, "maintenance.health.oom", "journalctl", err)
		return nil, false
	}
	var ks []model.OOMKill
	for _, jl := range journalLines(out) {
		msg, _ := field(jl.Message)
		if k, ok := parseKill(msg); ok && k.by == by {
			ks = append(ks, model.OOMKill{Time: micros(jl.Realtime), Program: cut(oneLine(k.program), nameMax), By: by})
		}
	}
	return ks, true
}

// ReadOOM lee los procesos que se mataron por falta de memoria en este
// arranque: los de systemd-oomd siempre y los del kernel solo si
// kernelKills (oom_kill de /proc/vmstat) es > 0. nil si systemd-oomd no se
// pudo leer. Partial: el usuario no ve el journal del sistema, así que las
// de systemd-oomd y los nombres del kernel pueden faltar.
func ReadOOM(env source.Env, kernelKills uint64, r *collect.Report) *model.OOMList {
	oomd, ok := readKills(env, OomdArgs, "systemd-oomd", r)
	if !ok {
		return nil
	}
	l := &model.OOMList{Oomd: len(oomd), List: append([]model.OOMKill{}, oomd...), Partial: JournalPartial(env)}
	if kernelKills > 0 {
		if ks, ok := readKills(env, KernelOOMArgs, "kernel", r); ok {
			l.List = append(l.List, ks...)
		}
	}
	sort.SliceStable(l.List, func(i, j int) bool { return l.List[i].Time.After(l.List[j].Time) })
	return l
}

// ReadHealth lee la salud de este arranque. full: errores del journal,
// crashes y OOM (Mantenimiento abierta); si no, solo los OOM (Memoria
// abierta). Nunca devuelve nil: lo que falla queda en nil con su
// unavailable.
func ReadHealth(env source.Env, full bool, kernelKills uint64, r *collect.Report) *model.Health {
	h := &model.Health{OOM: ReadOOM(env, kernelKills, r)}
	if full {
		h.Errors = ReadErrors(env, r)
		h.Crashes = ReadCrashes(env, r)
	}
	return h
}
