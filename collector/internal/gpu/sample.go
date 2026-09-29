package gpu

import (
	"strings"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// Sampler lee las métricas dinámicas. NVIDIA se consulta solo en los ticks
// lentos (nvidia-smi crea un proceso); en los demás se conserva la última
// lectura. Con Loop > 0 y GPUs siempre encendidas usa un nvidia-smi en bucle
// con ese período. Procs pide los procesos por GPU (solo con la sección GPU
// abierta).
type Sampler struct {
	Procs bool
	Loop  time.Duration
	last  map[string]smiRow
	procs map[string][]model.GPUProc
	loop  *smiLoop
	smiUn []model.Unavailable // faltantes de la última consulta a nvidia-smi
}

// Sample completa las métricas de gpus. slow habilita nvidia-smi.
func (s *Sampler) Sample(env source.Env, gpus []model.GPU, r *collect.Report, slow bool) {
	nvidia := false
	for i := range gpus {
		g := &gpus[i]
		resetDynamic(g)
		d := devDir(g.Card)
		if st, _ := env.FS.Read(d + "/power/runtime_status"); strings.TrimSpace(st) == "suspended" {
			g.Suspended = true
			continue
		}
		g.Suspended = false
		if g.Link != nil {
			g.Link.CurGTs = speed(env, d+"/current_link_speed")
			g.Link.CurGen = genOf(g.Link.CurGTs)
			w, _ := env.FS.ReadInt(d + "/current_link_width")
			g.Link.CurWidth = int(w)
		}
		switch g.Vendor {
		case "amd":
			amd(env, d, g)
		case "intel":
			hwmon(env, d, g)
			r.Absent("gpu.usage")
		case "nvidia":
			nvidia = true
		default:
			hwmon(env, d, g)
		}
	}
	if !nvidia {
		return
	}
	if !env.FS.Exists("/proc/driver/nvidia/version") {
		r.Missing("gpu.metrics", i18n.T(r.Lang, "the NVIDIA driver"))
		return
	}
	now := time.Now()
	if env.Now != nil {
		now = env.Now()
	}
	if rows := s.loopRows(env, gpus, now); rows != nil {
		s.last, s.smiUn = rows, nil
		if slow && s.Procs {
			s.queryProcs(env)
		}
	} else if slow {
		qr := &collect.Report{Lang: r.Lang}
		s.querySMI(env, qr)
		s.smiUn = qr.List
	}
	r.Keep(s.smiUn)
	for i := range gpus {
		g := &gpus[i]
		if g.Vendor != "nvidia" || g.Suspended {
			continue
		}
		if row, ok := s.last[busKey(g.PCI)]; ok {
			applySMI(g, row)
		}
		if s.Procs {
			g.Processes = s.procs[busKey(g.PCI)]
		}
	}
}

// resetDynamic borra las métricas dinámicas antes de leerlas de nuevo.
func resetDynamic(g *model.GPU) {
	g.UsagePct, g.VRAMUsedB, g.VRAMTotalB, g.GTTUsedB, g.GTTTotalB = nil, nil, nil, nil, nil
	g.TempC, g.PowerW, g.PowerLimitW, g.CoreMHz, g.MemMHz, g.FanPct = nil, nil, nil, nil, nil, nil
	g.Processes = nil
	if g.Link != nil {
		g.Link.CurGTs, g.Link.CurGen, g.Link.CurWidth = 0, 0, 0
	}
}

func amd(env source.Env, d string, g *model.GPU) {
	if v, err := env.FS.ReadFloat(d + "/gpu_busy_percent"); err == nil {
		g.UsagePct = &v
	}
	g.VRAMUsedB = u64(env, d+"/mem_info_vram_used")
	g.VRAMTotalB = u64(env, d+"/mem_info_vram_total")
	g.GTTUsedB = u64(env, d+"/mem_info_gtt_used")
	g.GTTTotalB = u64(env, d+"/mem_info_gtt_total")
	hwmon(env, d, g)
}

func u64(env source.Env, p string) *uint64 {
	v, err := env.FS.ReadInt(p)
	if err != nil || v < 0 {
		return nil
	}
	u := uint64(v)
	return &u
}

// hwmon lee la temperatura (prefiere "edge") y la potencia de la GPU.
func hwmon(env source.Env, d string, g *model.GPU) {
	dirs, _ := env.FS.Glob(d + "/hwmon/hwmon*")
	for _, h := range dirs {
		temps, _ := env.FS.Glob(h + "/temp*_input")
		for _, t := range temps {
			v, err := env.FS.ReadInt(t)
			if err != nil {
				continue
			}
			label, _ := env.FS.Read(strings.TrimSuffix(t, "_input") + "_label")
			if g.TempC == nil || label == "edge" {
				c := float64(v) / 1000
				g.TempC = &c
			}
		}
		for _, p := range []string{"/power1_average", "/power1_input"} {
			if v, err := env.FS.ReadInt(h + p); err == nil {
				w := float64(v) / 1e6
				g.PowerW = &w
				break
			}
		}
	}
}
