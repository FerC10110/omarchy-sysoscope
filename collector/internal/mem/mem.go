// Package mem lee el uso de memoria (como free(1)), la swap (con zram) y los
// módulos instalados (vía inxi, sin root).
package mem

import (
	"strconv"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// Read lee /proc/meminfo, /proc/swaps y PSI. Va en cada muestra.
func Read(env source.Env, r *collect.Report) *model.Memory {
	m := &model.Memory{}
	lines, err := env.FS.Lines("/proc/meminfo")
	if err != nil {
		r.FromErr("memory", "", err)
		return m
	}
	kv := map[string]uint64{}
	for _, l := range lines {
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		f := strings.Fields(v)
		if len(f) == 0 {
			continue
		}
		n, _ := strconv.ParseUint(f[0], 10, 64)
		if len(f) > 1 && f[1] == "kB" {
			n *= 1024
		}
		kv[k] = n
	}
	m.TotalB, m.FreeB, m.AvailableB = kv["MemTotal"], kv["MemFree"], kv["MemAvailable"]
	m.CacheB = kv["Buffers"] + kv["Cached"] + kv["SReclaimable"]
	if m.TotalB > m.FreeB+m.CacheB {
		m.UsedB = m.TotalB - m.FreeB - m.CacheB
	}
	m.DirtyB, m.ShmemB = kv["Dirty"], kv["Shmem"]
	if m.TotalB > 0 {
		m.UsedPct = float64(m.UsedB) / float64(m.TotalB) * 100
	}
	m.SwapTotalB = kv["SwapTotal"]
	if kv["SwapTotal"] > kv["SwapFree"] {
		m.SwapUsedB = kv["SwapTotal"] - kv["SwapFree"]
	}
	m.Swaps = swaps(env)
	if p, err := env.FS.Read("/proc/pressure/memory"); err == nil {
		m.PSI = collect.ParsePSI(p)
	}
	m.OOMKills = OOMKills(env)
	return m
}

func swaps(env source.Env) []model.Swap {
	lines, err := env.FS.Lines("/proc/swaps")
	if err != nil {
		return nil
	}
	var out []model.Swap
	for _, l := range lines[min(1, len(lines)):] {
		f := strings.Fields(l)
		if len(f) < 5 {
			continue
		}
		size, _ := strconv.ParseUint(f[2], 10, 64)
		used, _ := strconv.ParseUint(f[3], 10, 64)
		prio, _ := strconv.Atoi(f[4])
		s := model.Swap{Name: f[0], Kind: "partition", SizeB: size * 1024, UsedB: used * 1024, Priority: prio}
		if f[1] == "file" {
			s.Kind = "file"
		}
		if dev, ok := strings.CutPrefix(f[0], "/dev/zram"); ok {
			s.Kind = "zram"
			zram(env, "/sys/block/zram"+dev, &s)
		}
		out = append(out, s)
	}
	return out
}

// OOMKills es oom_kill de /proc/vmstat: los procesos que el kernel mató por
// falta de memoria desde el arranque. nil sin el archivo o sin el campo
// (kernels anteriores a 4.13). Una lectura por muestra.
func OOMKills(env source.Env) *uint64 {
	s, err := env.FS.Read("/proc/vmstat")
	if err != nil {
		return nil
	}
	const key = "\noom_kill "
	s = "\n" + s
	i := strings.Index(s, key)
	if i < 0 {
		return nil
	}
	v := s[i+len(key):]
	if j := strings.IndexByte(v, '\n'); j >= 0 {
		v = v[:j]
	}
	n, err := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
	if err != nil {
		return nil
	}
	return &n
}

func zram(env source.Env, dir string, s *model.Swap) {
	if algo, err := env.FS.Read(dir + "/comp_algorithm"); err == nil {
		if i := strings.Index(algo, "["); i >= 0 {
			if j := strings.Index(algo[i:], "]"); j > 0 {
				s.ZramAlgo = algo[i+1 : i+j]
			}
		}
	}
	if st, err := env.FS.Read(dir + "/mm_stat"); err == nil {
		f := strings.Fields(st)
		if len(f) >= 2 {
			orig, _ := strconv.ParseFloat(f[0], 64)
			compr, _ := strconv.ParseFloat(f[1], 64)
			if compr > 0 {
				ratio := orig / compr
				s.ZramRatio = &ratio
			}
		}
	}
}
