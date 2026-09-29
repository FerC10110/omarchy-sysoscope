package apps

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// Sampler lee las aplicaciones. Entre lecturas guarda lo necesario para las
// tasas (CPU, red, GPU por fdinfo) y lo último de las fuentes lentas.
type Sampler struct {
	UID  uint32
	Self map[int]bool // el colector y sus ancestros
	// Disk pide el disco por app (/proc/<pid>/io de cada proceso): solo con
	// Aplicaciones abierta, no en los tops de CPU, Memoria, GPU y Red.
	Disk bool

	cache   map[int]meta
	cpuLast map[pkey]uint64
	at      time.Time

	net     netMeter
	netRate map[int][2]float64
	netOK   bool

	gpuPct  map[int]float64
	gpuOK   bool
	drmLast map[int]uint64
	drmAt   time.Time

	ioLast map[pkey]ioBase // io de la lectura anterior; nil si fue sin Disk

	unSlow []model.Unavailable
}

// Sample lee procesos y ventanas; con slow también ss y nvidia-smi. threads
// son las CPUs lógicas: 100 % es toda la máquina.
func (s *Sampler) Sample(env source.Env, gpus []model.GPU, threads int, slow bool, r *collect.Report) *model.Apps {
	now := env.Now()
	if s.cache == nil {
		s.cache = map[int]meta{}
	}
	ps := readProcs(env.FS, s.UID, s.cache)
	var counts map[pkey]ioCount
	var sub map[pkey]ioCount
	if s.Disk {
		// Las restas pendientes dependen solo de los procesos vivos y de la
		// base: van antes de leer, porque un proceso con una resta se lee
		// aunque sus ticks no hayan cambiado (ver readDisk).
		alive := make(map[pkey]bool, len(ps))
		for _, p := range ps {
			alive[p.pkey] = true
		}
		sub = reaped(s.ioLast, alive)
		counts = readDisk(env.FS, ps, s.cpuLast, s.ioLast, sub, r)
	}
	hy, err := readHypr(env.Hypr)
	wins := map[int]string{}
	if err != nil {
		msg := i18n.T(r.Lang, "Hyprland is not running")
		if !errors.Is(err, source.ErrNoHyprland) {
			msg = i18n.F(r.Lang, "could not read: %s", err)
		}
		r.Note("apps.windows", "not_present", msg)
		hy = nil
	} else {
		wins = hy.classes()
	}
	gs := groupProcs(ps, wins, s.Self)
	if slow {
		rs := &collect.Report{Lang: r.Lang}
		s.readNet(env, now, rs)
		s.readGPU(env, gpus, windowedPIDs(gs, wins), now, rs)
		s.unSlow = rs.List
	}
	r.Keep(s.unSlow)

	dt := now.Sub(s.at).Seconds()
	next := make(map[pkey]uint64, len(ps))
	out := &model.Apps{List: make([]model.App, 0, len(gs))}
	if hy != nil {
		out.Monitors = hy.monitorNames()
	}
	for _, g := range gs {
		a := model.App{ID: g.id, Name: g.name, Procs: len(g.procs), Protected: g.protected}
		var ticks, rx, tx, gpu, dr, dw float64
		disk := false
		for _, p := range g.procs {
			// Disco: el proceso cuenta solo si se pudo leer ahora y estaba
			// en la lectura anterior (un pid reusado es otro pkey; uno nuevo
			// no aporta ni da dato). A la diferencia se le resta lo de los
			// hijos que recogió desde entonces.
			if c, ok := counts[p.pkey]; ok {
				if prev, ok := s.ioLast[p.pkey]; ok {
					disk = true
					k := sub[p.pkey]
					dr += float64(delta(c.read, prev.read+k.read))
					dw += float64(delta(c.write, prev.write+k.write))
				}
			}
			next[p.pkey] = p.cpu
			a.MemB += p.rssB
			if prev, ok := s.cpuLast[p.pkey]; ok && p.cpu >= prev {
				ticks += float64(p.cpu - prev)
			}
			if hy != nil {
				a.Windows = append(a.Windows, hy.windows[p.pid]...)
			}
			n := s.netRate[p.pid]
			rx, tx = rx+n[0], tx+n[1]
			gpu += s.gpuPct[p.pid]
		}
		// Sin scope, el nombre sale de la clase de su primera ventana (en
		// orden estable: el nombre no cambia con el foco); el scope ya trae
		// un nombre mejor.
		if strings.HasPrefix(g.id, "comm:") && len(a.Windows) > 0 && a.Windows[0].Class != "" {
			a.Name = a.Windows[0].Class
		}
		if !s.at.IsZero() && dt > 0 && threads > 0 {
			a.CPUPct = round1(ticks / clkTck / dt * 100 / float64(threads))
		}
		// Sin ningún proceso con dos lecturas, la app queda sin dato ("—"),
		// no en 0.
		if disk && !s.at.IsZero() && dt > 0 {
			rd, wr := dr/dt, dw/dt
			a.DiskReadBps, a.DiskWriteBps = &rd, &wr
		}
		if s.netOK {
			a.NetRxBps, a.NetTxBps = &rx, &tx
		}
		if s.gpuOK {
			v := round1(math.Min(100, gpu))
			a.GPUPct = &v
		}
		out.TotalCPUPct += a.CPUPct
		out.TotalMemB += a.MemB
		out.List = append(out.List, a)
	}
	out.TotalCPUPct = round1(out.TotalCPUPct)
	// Las apps con ventana van en orden estable (nombre, id): son filas
	// elegibles y no se mueven bajo el cursor. Las sin ventana son un
	// ranking: por CPU y memoria.
	sort.SliceStable(out.List, func(i, j int) bool {
		a, b := out.List[i], out.List[j]
		if (len(a.Windows) > 0) != (len(b.Windows) > 0) {
			return len(a.Windows) > 0
		}
		if len(a.Windows) > 0 {
			if na, nb := strings.ToLower(a.Name), strings.ToLower(b.Name); na != nb {
				return na < nb
			}
			return a.ID < b.ID
		}
		if a.CPUPct != b.CPUPct {
			return a.CPUPct > b.CPUPct
		}
		return a.MemB > b.MemB
	})
	s.cpuLast, s.at, s.ioLast = next, now, diskBase(ps, counts)
	return out
}

// Reset olvida la lectura anterior: la próxima es como la primera, sin
// tasas (CPU, red, GPU por fdinfo, disco).
func (s *Sampler) Reset() {
	s.cpuLast, s.at, s.ioLast = nil, time.Time{}, nil
	s.net, s.netRate, s.netOK = netMeter{}, nil, false
	s.drmLast, s.drmAt = nil, time.Time{}
}

// DropDisk olvida solo la base del disco: al salir de Aplicaciones, la
// primera lectura al volver no promedia el disco sobre todo el rato afuera.
// La CPU conserva su base (los tops la siguen usando).
func (s *Sampler) DropDisk() { s.ioLast = nil }

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// windowedPIDs son los procesos de las apps con ventana: solo a ellos se
// les lee fdinfo (recorrer cada fd de cada proceso cuesta demasiado).
func windowedPIDs(gs []group, wins map[int]string) []int {
	var out []int
	for _, g := range gs {
		has := false
		for _, p := range g.procs {
			_, ok := wins[p.pid]
			has = has || ok
		}
		if has {
			for _, p := range g.procs {
				out = append(out, p.pid)
			}
		}
	}
	return out
}

func (s *Sampler) readNet(env source.Env, now time.Time, r *collect.Report) {
	out, err := env.Run.Run(context.Background(), "ss", SSArgs...)
	if err != nil {
		s.netOK, s.netRate = false, nil
		r.FromErr("apps.net", "ss", err)
		return
	}
	first := s.net.at.IsZero()
	s.netRate = s.net.rates(parseSS(string(out)), now)
	s.netOK = !first
}

// readGPU pide pmon a NVIDIA con el driver propietario y lee fdinfo para AMD,
// Intel y nouveau. Una GPU dormida no se despierta: ni pmon ni sus fds.
func (s *Sampler) readGPU(env source.Env, gpus []model.GPU, pids []int, now time.Time, r *collect.Report) {
	s.gpuPct, s.gpuOK = map[int]float64{}, false
	var nvidia, drm, asleep bool
	skip := map[string]bool{}
	proprietary := env.FS.Exists("/proc/driver/nvidia/version")
	for _, g := range gpus {
		switch {
		case g.Suspended:
			asleep = true
			skip[g.PCI] = true
		case g.Vendor == "nvidia" && proprietary:
			nvidia = true
		case g.Vendor == "nvidia" || g.Vendor == "amd" || g.Vendor == "intel":
			drm = true // nouveau también da fdinfo
		}
	}
	if nvidia {
		out, err := env.Run.Run(context.Background(), "nvidia-smi", PmonArgs...)
		if err != nil {
			r.FromErr("apps.gpu", "nvidia-smi", err)
		} else {
			for pid, v := range parsePmon(string(out)) {
				s.gpuPct[pid] += v
			}
			s.gpuOK = true
		}
	}
	if drm {
		busy := drmBusy(env.FS, pids, skip)
		if dt := now.Sub(s.drmAt).Seconds(); s.drmLast != nil && dt > 0 {
			for pid, ns := range busy {
				if prev, ok := s.drmLast[pid]; ok && ns >= prev {
					s.gpuPct[pid] += float64(ns-prev) / (dt * 1e9) * 100
				}
			}
		}
		s.drmLast, s.drmAt = busy, now
		s.gpuOK = true
	}
	if !s.gpuOK && asleep {
		r.Note("apps.gpu", "not_present", i18n.T(r.Lang, "the GPU is asleep; it is not woken up to read it"))
	}
}
