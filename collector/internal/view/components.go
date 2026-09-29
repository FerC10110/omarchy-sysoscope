package view

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

func init() {
	builders["cpu"] = cpuView
	builders["memory"] = memoryView
	builders["gpu"] = gpuView
}

// rows descarta las filas sin valor, para que ningún kv muestre un hueco.
func rows(rs ...[2]string) [][2]string {
	out := [][2]string{}
	for _, r := range rs {
		if r[1] != "" {
			out = append(out, r)
		}
	}
	return out
}

func mhz(v *float64) string {
	if v == nil {
		return ""
	}
	return MHz(*v)
}

func loadText(l [3]float64) string { return fmt.Sprintf("%.2f · %.2f · %.2f", l[0], l[1], l[2]) }

// peak es el máximo de los valores recientes, o false si no hay.
func peak(vs []float64) (float64, bool) {
	if len(vs) == 0 {
		return 0, false
	}
	return slices.Max(vs), true
}

func appendUnavailable(bs []Block, s *model.Snapshot, prefix string) []Block {
	if u := Unavailable(s, prefix); u != nil {
		bs = append(bs, *u)
	}
	return bs
}

func cpuView(s *model.Snapshot, o Options) []Block {
	l := o.Lang
	c := s.CPU
	if c == nil {
		return appendUnavailable([]Block{}, s, "cpu")
	}
	threads := fmt.Sprintf("%d / %d", c.Cores, c.Threads)
	if c.Sockets > 1 {
		threads += " · " + i18n.F(l, "%d sockets", c.Sockets)
	}
	baseMax := mhz(c.BaseMHz)
	if m := mhz(c.MaxMHz); m != "" {
		if baseMax != "" {
			baseMax += " / "
		}
		baseMax += m
	}
	boost := ""
	if c.Boost != nil {
		boost = i18n.T(l, "off")
		if *c.Boost {
			boost = i18n.T(l, "on")
		}
	}
	bs := []Block{KV(i18n.T(l, "Processor"), rows(
		[2]string{i18n.T(l, "Model"), c.Model},
		[2]string{i18n.T(l, "Architecture"), c.Arch},
		[2]string{i18n.T(l, "Cores / threads"), threads},
		[2]string{i18n.T(l, "Base / max"), baseMax},
		[2]string{i18n.T(l, "Microcode"), c.Microcode},
		[2]string{i18n.T(l, "Governor"), c.Governor},
		[2]string{i18n.T(l, "Driver"), c.Driver},
		[2]string{"EPP", c.EPP},
		[2]string{i18n.T(l, "Boost"), boost},
	)...)}

	if len(c.Caches) > 0 {
		var cr [][2]string
		for _, k := range c.Caches {
			name := "L" + strconv.Itoa(k.Level)
			switch k.Type {
			case "Data":
				name += "d"
			case "Instruction":
				name += "i"
			}
			cr = append(cr, [2]string{name, fmt.Sprintf("%s × %d", Bytes(k.SizeB), k.Instances)})
		}
		bs = append(bs, KV(i18n.T(l, "Caches"), cr...))
	}

	var now []Block
	if c.UsagePct != nil {
		now = append(now, Meter(i18n.T(l, "Usage"), *c.UsagePct/100, Pct(*c.UsagePct), Level(*c.UsagePct, 70, 90)),
			Spark("", o.recent("cpu.usage"), ""))
	}
	if c.TempC != nil {
		t := Temp(*c.TempC, o.Units)
		if p, ok := peak(o.recent("cpu.temp")); ok {
			t += " · " + i18n.F(l, "peak %s", Temp(p, o.Units))
		}
		if c.TempSensor != "" {
			t += " (" + c.TempSensor + ")"
		}
		now = append(now, Meter(i18n.T(l, "Temperature"), *c.TempC/100, t, Level(*c.TempC, 80, 90)))
	}
	pressure := ""
	if c.PSI != nil {
		pressure = Pct(c.PSI.Some10)
	}
	now = append(now, KV("", rows(
		[2]string{i18n.T(l, "Average clock"), mhz(c.AvgMHz)},
		[2]string{i18n.T(l, "Load"), loadText(c.Load)},
		[2]string{i18n.T(l, "Pressure (10 s)"), pressure},
	)...))
	bs = append(bs, Group(i18n.T(l, "Now"), now...))
	bs = append(bs, coreMap(c, l)...)
	return appendUnavailable(bs, s, "cpu")
}

// coreMap arma un mapa de núcleos por grupo de L3 (un CCX en AMD).
func coreMap(c *model.CPU, l string) []Block {
	if len(c.PerCPUPct) == 0 || len(c.Topology) == 0 {
		return nil
	}
	groups := map[int][]model.LogicalCPU{}
	var keys []int
	for _, t := range c.Topology {
		if _, ok := groups[t.L3]; !ok {
			keys = append(keys, t.L3)
		}
		groups[t.L3] = append(groups[t.L3], t)
	}
	slices.Sort(keys)
	var out []Block
	for i, k := range keys {
		ts := groups[k]
		slices.SortFunc(ts, func(a, b model.LogicalCPU) int { return a.ID - b.ID })
		var cells []Cell
		for _, t := range ts {
			if t.ID >= len(c.PerCPUPct) {
				continue
			}
			p := c.PerCPUPct[t.ID]
			cells = append(cells, Cell{Label: strconv.Itoa(t.ID), Value: p / 100, Level: Level(p, 70, 90)})
		}
		title := i18n.T(l, "Cores")
		if len(keys) > 1 {
			title = i18n.F(l, "CCX %d", i+1)
		}
		out = append(out, Cores(title, cells))
	}
	return out
}

func memoryView(s *model.Snapshot, o Options) []Block {
	l := o.Lang
	m := s.Memory
	if m == nil {
		return appendUnavailable([]Block{}, s, "memory")
	}
	breakdown := [][2]string{
		{i18n.T(l, "Apps"), Bytes(m.UsedB)},
		{i18n.T(l, "Cache"), Bytes(m.CacheB)},
		{i18n.T(l, "Free"), Bytes(m.FreeB)},
		{i18n.T(l, "Available"), Bytes(m.AvailableB)},
		{i18n.T(l, "Dirty"), Bytes(m.DirtyB)},
		{i18n.T(l, "Shared"), Bytes(m.ShmemB)},
	}
	if m.OOMKills != nil {
		breakdown = append(breakdown, [2]string{i18n.T(l, "Killed for lack of memory (since boot)"), oomRow(s, l)})
	}
	var bs []Block
	for _, w := range memoryWarnings(s, l) {
		bs = append(bs, Text(w.Text, "warn")) // el mismo aviso del Resumen, arriba
	}
	bs = append(bs,
		Meter(i18n.T(l, "Used"), m.UsedPct/100, i18n.F(l, "%s of %s", Bytes(m.UsedB), Bytes(m.TotalB)), Level(m.UsedPct, 80, 90)),
		Spark("", o.recent("mem.used_pct"), ""),
		KV(i18n.T(l, "Breakdown"), breakdown...),
	)
	if m.SwapTotalB > 0 {
		pct := float64(m.SwapUsedB) / float64(m.SwapTotalB) * 100
		bs = append(bs, Meter(i18n.T(l, "Swap"), pct/100, i18n.F(l, "%s of %s", Bytes(m.SwapUsedB), Bytes(m.SwapTotalB)), Level(pct, 50, 80)))
		var sr []Row
		for _, w := range m.Swaps {
			kind := w.Kind
			if w.Kind == "zram" && w.ZramAlgo != "" && w.ZramRatio != nil {
				kind = fmt.Sprintf("zram (%s, %.1f×)", w.ZramAlgo, *w.ZramRatio)
			}
			sr = append(sr, Row{ID: w.Name, Cells: []string{w.Name, kind, Bytes(w.SizeB), Bytes(w.UsedB), strconv.Itoa(w.Priority)}})
		}
		bs = append(bs, Table(i18n.T(l, "Swap devices"),
			[]string{i18n.T(l, "Name"), i18n.T(l, "Kind"), i18n.T(l, "Size"), i18n.T(l, "Used"), i18n.T(l, "Priority")}, sr, false))
	}
	if len(m.Modules) == 0 {
		return appendUnavailable(bs, s, "memory.modules")
	}
	slots := ""
	if m.Slots > 0 {
		slots = i18n.F(l, "%d of %d", len(m.Modules), m.Slots)
	}
	if r := rows([2]string{i18n.T(l, "Slots"), slots}, [2]string{"ECC", m.ECC}); len(r) > 0 {
		bs = append(bs, KV("", r...))
	}
	var mr []Row
	for _, d := range m.Modules {
		speed := "—"
		switch {
		case d.ActualMTs != nil && d.SpecMTs != nil:
			speed = fmt.Sprintf("%d / %d MT/s", *d.ActualMTs, *d.SpecMTs)
		case d.ActualMTs != nil:
			speed = fmt.Sprintf("%d MT/s", *d.ActualMTs)
		case d.SpecMTs != nil:
			speed = fmt.Sprintf("%d MT/s", *d.SpecMTs)
		}
		mr = append(mr, Row{ID: d.Slot, Cells: []string{d.Slot, d.Type, Bytes(d.SizeB), speed}})
	}
	return append(bs, Table(i18n.T(l, "Modules"),
		[]string{i18n.T(l, "Slot"), i18n.T(l, "Type"), i18n.T(l, "Size"), i18n.T(l, "Speed")}, mr, false))
}

// pcieText: "Gen 1 x16 now · Gen 4 x16 max". Una GPU que baja el enlace en
// reposo es normal, así que no se juzga.
func pcieText(k *model.PCIeLink, l string) string {
	if k == nil {
		return ""
	}
	max := i18n.F(l, "Gen %d x%d max", k.MaxGen, k.MaxWidth)
	if k.CurGen == 0 {
		return max
	}
	return i18n.F(l, "Gen %d x%d now", k.CurGen, k.CurWidth) + " · " + max
}

func gpuView(s *model.Snapshot, o Options) []Block {
	l := o.Lang
	var bs []Block
	for _, g := range s.GPUs {
		bs = append(bs, Group(g.Name, gpuBlocks(g, o)...))
	}
	if len(bs) == 0 {
		if u := Unavailable(s, "gpu"); u != nil {
			return []Block{*u}
		}
		return []Block{Text(i18n.T(l, "No graphics card found"), "ok")}
	}
	return appendUnavailable(bs, s, "gpu")
}

func gpuBlocks(g model.GPU, o Options) []Block {
	l := o.Lang
	driver := g.Driver
	if g.DriverVersion != "" {
		driver += " " + g.DriverVersion
	}
	if g.Suspended {
		return []Block{
			Text(i18n.T(l, "Asleep (runtime suspend): not woken up to read it"), "ok"),
			KV("", rows(
				[2]string{i18n.T(l, "PCI slot"), g.PCI},
				[2]string{i18n.T(l, "PCIe link"), pcieText(g.Link, l)},
				[2]string{i18n.T(l, "Driver"), driver},
				[2]string{"VBIOS", g.VBIOS},
			)...),
		}
	}
	var bs []Block
	if g.UsagePct != nil {
		bs = append(bs, Meter(i18n.T(l, "Usage"), *g.UsagePct/100, Pct(*g.UsagePct), Level(*g.UsagePct, 80, 95)))
	}
	if g.VRAMUsedB != nil && g.VRAMTotalB != nil && *g.VRAMTotalB > 0 {
		label, used, total := "VRAM", *g.VRAMUsedB, *g.VRAMTotalB
		if g.Integrated && g.GTTUsedB != nil && g.GTTTotalB != nil {
			label, used, total = "VRAM + GTT", used+*g.GTTUsedB, total+*g.GTTTotalB
		}
		pct := float64(used) / float64(total) * 100
		bs = append(bs, Meter(label, pct/100, i18n.F(l, "%s of %s", Bytes(used), Bytes(total)), Level(pct, 80, 95)))
	}
	if g.TempC != nil {
		bs = append(bs, Meter(i18n.T(l, "Temperature"), *g.TempC/100, Temp(*g.TempC, o.Units), Level(*g.TempC, 80, 90)))
	}
	power, fan := "", ""
	if g.PowerW != nil {
		power = Watts(*g.PowerW)
		if g.PowerLimitW != nil {
			power = i18n.F(l, "%s of %s", power, Watts(*g.PowerLimitW))
		}
	}
	if g.FanPct != nil {
		fan = Pct(*g.FanPct)
	}
	bs = append(bs, KV("", rows(
		[2]string{i18n.T(l, "Power"), power},
		[2]string{i18n.T(l, "Core clock"), mhz(g.CoreMHz)},
		[2]string{i18n.T(l, "Memory clock"), mhz(g.MemMHz)},
		[2]string{i18n.T(l, "Fan"), fan},
		[2]string{i18n.T(l, "PCIe link"), pcieText(g.Link, l)},
		[2]string{i18n.T(l, "Driver"), driver},
		[2]string{"VBIOS", g.VBIOS},
	)...))
	if len(g.Processes) > 0 {
		var pr []Row
		for _, p := range g.Processes {
			id := strconv.Itoa(p.PID)
			pr = append(pr, Row{ID: id, Cells: []string{id, p.Name, Bytes(p.VRAMB)}})
		}
		bs = append(bs, Table(i18n.T(l, "Processes"), []string{"PID", i18n.T(l, "Name"), "VRAM"}, pr, false))
	}
	return bs
}
