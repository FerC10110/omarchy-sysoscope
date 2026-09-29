// Package cpu lee el inventario del procesador (modelo, núcleos, cachés,
// capacidades, vulnerabilidades, topología) y su uso en vivo.
package cpu

import (
	"fmt"
	"path"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

const sysCPU = "/sys/devices/system/cpu"

// Static lee lo que no cambia durante el arranque.
func Static(env source.Env, r *collect.Report) *model.CPU {
	c := &model.CPU{}
	info := parseCPUInfo(env)
	c.Model, c.Vendor, c.Microcode = info.model, info.vendor, info.microcode
	c.Arch = arch(env)
	c.Capabilities = capabilities(c.Arch, c.Vendor, info.flags)

	ids := onlineCPUs(env)
	if len(ids) == 0 {
		for i := 0; i < info.count; i++ {
			ids = append(ids, i)
		}
	}
	c.Threads = len(ids)
	topology(env, c, ids)
	if len(c.Topology) == 0 {
		c.Sockets = max(info.sockets, 1)
		c.Cores = info.cores * c.Sockets
		if c.Cores == 0 {
			c.Cores = c.Threads
		}
	}
	c.Caches = caches(env, ids)
	freqStatic(env, c, r)
	c.Vulnerabilities = vulnerabilities(env)
	return c
}

type cpuInfo struct {
	model, vendor, microcode string
	flags                    map[string]bool
	count, cores, sockets    int
}

func parseCPUInfo(env source.Env) cpuInfo {
	ci := cpuInfo{flags: map[string]bool{}}
	lines, _ := env.FS.Lines("/proc/cpuinfo")
	phys := map[string]bool{}
	for _, l := range lines {
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "processor":
			ci.count++
		case "model name":
			if ci.model == "" {
				ci.model = v
			}
		case "vendor_id":
			ci.vendor = vendorName(v)
		case "CPU implementer":
			if ci.vendor == "" {
				ci.vendor = "ARM"
			}
		case "Model", "Hardware":
			if ci.model == "" {
				ci.model = v
			}
		case "microcode":
			ci.microcode = v
		case "flags", "Features":
			for _, f := range strings.Fields(v) {
				ci.flags[f] = true
			}
		case "cpu cores":
			ci.cores, _ = strconv.Atoi(v)
		case "physical id":
			phys[v] = true
		}
	}
	ci.sockets = len(phys)
	return ci
}

func vendorName(id string) string {
	switch id {
	case "AuthenticAMD", "HygonGenuine":
		return "AMD"
	case "GenuineIntel":
		return "Intel"
	}
	return id
}

func arch(env source.Env) string {
	if a, err := env.FS.Read("/proc/sys/kernel/arch"); err == nil && a != "" {
		return strings.TrimSpace(a)
	}
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	}
	return runtime.GOARCH
}

// parseList interpreta listas del kernel como "0-2,12-14".
func parseList(s string) []int {
	var out []int
	for _, part := range strings.Split(strings.TrimSpace(s), ",") {
		if part == "" {
			continue
		}
		a, b, isRange := strings.Cut(part, "-")
		lo, err := strconv.Atoi(a)
		if err != nil {
			continue
		}
		hi := lo
		if isRange {
			if hi, err = strconv.Atoi(b); err != nil {
				continue
			}
		}
		for i := lo; i <= hi; i++ {
			out = append(out, i)
		}
	}
	return out
}

func onlineCPUs(env source.Env) []int {
	s, err := env.FS.Read(sysCPU + "/online")
	if err != nil {
		return nil
	}
	return parseList(s)
}

func cpuDir(id int) string { return fmt.Sprintf("%s/cpu%d", sysCPU, id) }

func topology(env source.Env, c *model.CPU, ids []int) {
	l3groups := map[string]int{}
	cores := map[[2]int]bool{}
	pkgs := map[int]bool{}
	for _, id := range ids {
		d := cpuDir(id)
		core, err := env.FS.ReadInt(d + "/topology/core_id")
		if err != nil {
			continue
		}
		pkg, _ := env.FS.ReadInt(d + "/topology/physical_package_id")
		die, _ := env.FS.ReadInt(d + "/topology/die_id")
		sib, _ := env.FS.Read(d + "/topology/thread_siblings_list")
		lc := model.LogicalCPU{ID: id, Core: int(core), Package: int(pkg), Die: int(die), Sibling: parseList(sib), L3: -1}
		if shared, err := env.FS.Read(l3Dir(env, d) + "/shared_cpu_list"); err == nil {
			g, ok := l3groups[shared]
			if !ok {
				g = len(l3groups)
				l3groups[shared] = g
			}
			lc.L3 = g
		}
		if mx, err := env.FS.ReadInt(d + "/cpufreq/cpuinfo_max_freq"); err == nil {
			v := float64(mx) / 1000
			lc.MaxMHz = &v
		}
		cores[[2]int{int(pkg), int(core)}] = true
		pkgs[int(pkg)] = true
		c.Topology = append(c.Topology, lc)
	}
	if len(c.Topology) > 0 {
		c.Cores, c.Sockets = len(cores), len(pkgs)
	}
}

// l3Dir busca el índice de caché de nivel 3 de una CPU ("" si no hay).
func l3Dir(env source.Env, cpu string) string {
	idx, _ := env.FS.Glob(cpu + "/cache/index*")
	for _, i := range idx {
		if lv, err := env.FS.ReadInt(i + "/level"); err == nil && lv == 3 {
			return i
		}
	}
	return cpu + "/cache/none"
}

func caches(env source.Env, ids []int) []model.Cache {
	type key struct {
		level int
		typ   string
	}
	sizes := map[key]uint64{}
	groups := map[key]map[string]bool{}
	for _, id := range ids {
		idx, _ := env.FS.Glob(cpuDir(id) + "/cache/index*")
		for _, i := range idx {
			lv, err := env.FS.ReadInt(i + "/level")
			if err != nil {
				continue
			}
			typ, _ := env.FS.Read(i + "/type")
			size, _ := env.FS.Read(i + "/size")
			shared, _ := env.FS.Read(i + "/shared_cpu_list")
			k := key{int(lv), strings.TrimSpace(typ)}
			sizes[k] = parseSize(size)
			if groups[k] == nil {
				groups[k] = map[string]bool{}
			}
			if shared == "" {
				shared = path.Base(i) + strconv.Itoa(id)
			}
			groups[k][shared] = true
		}
	}
	out := make([]model.Cache, 0, len(sizes))
	for k, s := range sizes {
		out = append(out, model.Cache{Level: k.level, Type: k.typ, SizeB: s, Instances: len(groups[k])})
	}
	order := map[string]int{"Data": 0, "Instruction": 1, "Unified": 2}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Level != out[j].Level {
			return out[i].Level < out[j].Level
		}
		return order[out[i].Type] < order[out[j].Type]
	})
	return out
}

// parseSize interpreta "32K", "8192K", "1M" o bytes sin sufijo.
func parseSize(s string) uint64 {
	s = strings.TrimSpace(s)
	mult := uint64(1)
	switch {
	case strings.HasSuffix(s, "K"):
		mult, s = 1<<10, strings.TrimSuffix(s, "K")
	case strings.HasSuffix(s, "M"):
		mult, s = 1<<20, strings.TrimSuffix(s, "M")
	case strings.HasSuffix(s, "G"):
		mult, s = 1<<30, strings.TrimSuffix(s, "G")
	}
	n, _ := strconv.ParseUint(s, 10, 64)
	return n * mult
}

func freqStatic(env source.Env, c *model.CPU, r *collect.Report) {
	d := sysCPU + "/cpu0/cpufreq"
	if !env.FS.Exists(d) {
		r.Absent("cpu.freq")
		return
	}
	if v, err := env.FS.ReadInt(d + "/cpuinfo_max_freq"); err == nil {
		f := float64(v) / 1000
		c.MaxMHz = &f
	}
	if v, err := env.FS.ReadInt(d + "/cpuinfo_min_freq"); err == nil {
		f := float64(v) / 1000
		c.MinMHz = &f
	}
	if v, err := env.FS.ReadInt(d + "/base_frequency"); err == nil {
		f := float64(v) / 1000
		c.BaseMHz = &f
	}
	c.Governor, _ = env.FS.Read(d + "/scaling_governor")
	c.Driver, _ = env.FS.Read(d + "/scaling_driver")
	c.EPP, _ = env.FS.Read(d + "/energy_performance_preference")
	if v, err := env.FS.ReadInt(sysCPU + "/cpufreq/boost"); err == nil {
		b := v == 1
		c.Boost = &b
	} else if v, err := env.FS.ReadInt(sysCPU + "/intel_pstate/no_turbo"); err == nil {
		b := v == 0
		c.Boost = &b
	}
}

func vulnerabilities(env source.Env) []model.Vulnerability {
	names, err := env.FS.Dir(sysCPU + "/vulnerabilities")
	if err != nil {
		return nil
	}
	out := make([]model.Vulnerability, 0, len(names))
	for _, n := range names {
		txt, err := env.FS.Read(sysCPU + "/vulnerabilities/" + n)
		if err != nil {
			continue
		}
		out = append(out, model.Vulnerability{Name: n, State: vulnState(txt), Detail: txt})
	}
	return out
}

func vulnState(s string) string {
	switch {
	case strings.HasPrefix(s, "Not affected"):
		return "not_affected"
	case strings.HasPrefix(s, "Mitigation"):
		return "mitigated"
	case strings.HasPrefix(s, "Vulnerable"):
		return "vulnerable"
	}
	return "unknown"
}
