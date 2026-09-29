package view

import (
	"strings"
	"testing"
)

func build(t *testing.T, id, fixture string, o Options) *View {
	t.Helper()
	s, e := snap(t, fixture)
	if o.Lang == "" {
		o.Lang = "en"
	}
	if o.Recent == nil {
		o.Recent = e.Recent
	}
	v, err := Build(id, s, o)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// walk recorre los bloques, entrando en los grupos.
func walk(bs []Block, f func(Block)) {
	for _, b := range bs {
		f(b)
		walk(b.Blocks, f)
	}
}

func noEmptyValues(t *testing.T, v *View) {
	t.Helper()
	walk(v.Blocks, func(b Block) {
		if b.Type == "kv" {
			for _, r := range kvRows(b) {
				if strings.TrimSpace(r[1]) == "" {
					t.Errorf("%s: row %q has an empty value", v.ID, r[0])
				}
			}
		}
	})
}

func TestCPUView(t *testing.T) {
	v := build(t, "cpu", "threadripper", Options{})
	if v.Title != "CPU" || body(v)[0].Type != "kv" ||
		!hasRow(body(v)[0], "Model", "AMD Ryzen Threadripper 1920X 12-Core Processor") || !hasRow(body(v)[0], "Cores / threads", "12 / 24") {
		t.Fatalf("processor block = %+v", body(v)[0])
	}
	var cores []Block
	walk(v.Blocks, func(b Block) {
		if b.Type == "cores" {
			cores = append(cores, b)
		}
	})
	if len(cores) != 4 {
		t.Fatalf("core map groups = %d, want 4 CCX", len(cores))
	}
	for _, c := range cores {
		if len(c.Cells) != 6 {
			t.Errorf("%s has %d cells, want 6", c.Title, len(c.Cells))
		}
	}
	noEmptyValues(t, v)
}

func TestMemoryView(t *testing.T) {
	v := build(t, "memory", "threadripper", Options{})
	var modules *Block
	walk(v.Blocks, func(b Block) {
		if b.Type == "table" && b.Title == "Modules" {
			modules = &b
		}
	})
	if modules == nil || len(modules.Rows.([]Row)) != 3 {
		t.Fatalf("modules table = %+v", modules)
	}
	noEmptyValues(t, v)
}

func TestGPUView(t *testing.T) {
	v := build(t, "gpu", "threadripper", Options{})
	g := body(v)[0]
	if g.Type != "group" || g.Title != "NVIDIA GeForce RTX 3080" {
		t.Fatalf("gpu group = %+v", g)
	}
	link := ""
	walk(g.Blocks, func(b Block) {
		for _, r := range kvRows(b) {
			if r[0] == "PCIe link" {
				link = r[1]
			}
		}
	})
	if !strings.HasPrefix(link, "Gen 1 x16 now") {
		t.Errorf("PCIe link = %q", link)
	}
	noEmptyValues(t, v)

	o := build(t, "gpu", "optimus", Options{})
	var nv *Block
	for i := range o.Blocks {
		if strings.Contains(o.Blocks[i].Title, "NVIDIA") || strings.Contains(o.Blocks[i].Title, "GeForce") {
			nv = &o.Blocks[i]
		}
	}
	if nv == nil {
		t.Fatalf("optimus groups = %+v", o.Blocks)
	}
	asleep, meters := false, 0
	walk(nv.Blocks, func(b Block) {
		if b.Type == "text" && strings.HasPrefix(b.Text, "Asleep (runtime suspend)") {
			asleep = true
		}
		if b.Type == "meter" {
			meters++
		}
	})
	if !asleep || meters != 0 {
		t.Errorf("suspended dGPU: asleep %v, meters %d", asleep, meters)
	}

	m := build(t, "gpu", "minimal", Options{})
	if len(m.Blocks) != 1 || m.Blocks[0].Type != "text" || m.Blocks[0].Text != "No graphics card found" {
		t.Errorf("minimal gpu view = %+v", m.Blocks)
	}
}

func groups(v *View) []Block {
	var out []Block
	for _, b := range v.Blocks {
		if b.Type == "group" {
			out = append(out, b)
		}
	}
	return out
}

func kvValue(bs []Block, title, key string) string {
	for _, b := range bs {
		if b.Type == "kv" && (title == "*" || b.Title == title) {
			for _, r := range kvRows(b) {
				if r[0] == key {
					return r[1]
				}
			}
		}
	}
	return ""
}

func TestStorageView(t *testing.T) {
	v := build(t, "storage", "threadripper", Options{})
	gs := groups(v)
	if len(gs) != 6 {
		t.Fatalf("disk groups = %d, want 6", len(gs))
	}
	var sandisk, hdd *Block
	for i := range gs {
		switch {
		case strings.HasPrefix(gs[i].Title, "SanDisk"):
			sandisk = &gs[i]
		case strings.HasPrefix(gs[i].Title, "WDC WD120"):
			hdd = &gs[i]
		}
	}
	if sandisk == nil || hdd == nil {
		t.Fatalf("groups = %+v", gs)
	}
	if st := kvValue(sandisk.Blocks, "Health", "Status"); st != "Warnings" {
		t.Errorf("SanDisk status = %q", st)
	}
	warn := false
	for _, b := range sandisk.Blocks {
		if b.Type == "text" && b.Level == "warn" && strings.Contains(b.Text, "34 bad sectors") {
			warn = true
		}
	}
	if !warn {
		t.Errorf("SanDisk has no warning text: %+v", sandisk.Blocks)
	}
	if i := kvValue(hdd.Blocks, "*", "Interface"); i != "sata, HDD" {
		t.Errorf("HDD interface = %q", i)
	}
	root := false
	walk(v.Blocks, func(b Block) {
		if b.Type == "table" && b.Title == "Filesystems" {
			for _, r := range b.Rows.([]Row) {
				if strings.HasPrefix(r.Cells[0], "/") {
					root = true
				}
			}
		}
	})
	if !root {
		t.Error("no filesystem row with a mount")
	}
	noEmptyValues(t, v)
}

func TestNetworkView(t *testing.T) {
	v := build(t, "network", "threadripper", Options{})
	gs := groups(v)
	if len(gs) != 7 {
		t.Fatalf("interface groups = %d, want 7 (all but lo)", len(gs))
	}
	if gs[0].Title != "enp5s0" || kvValue(gs[0].Blocks, "*", "State") != "Up" {
		t.Errorf("first interface = %s, state %q", gs[0].Title, kvValue(gs[0].Blocks, "*", "State"))
	}
	noEmptyValues(t, v)
}

func TestSensorsView(t *testing.T) {
	v := build(t, "sensors", "threadripper", Options{})
	tdie := false
	for _, g := range groups(v) {
		if strings.HasPrefix(g.Title, "CPU") {
			walk(g.Blocks, func(b Block) {
				if b.Type == "table" {
					for _, r := range b.Rows.([]Row) {
						if r.Cells[0] == "Tdie" {
							tdie = true
						}
					}
				}
			})
		}
	}
	if !tdie {
		t.Error("no Tdie row in a CPU chip")
	}
	m := build(t, "sensors", "minimal", Options{})
	if len(m.Blocks) != 1 || m.Blocks[0].Type != "text" || !strings.HasPrefix(m.Blocks[0].Text, "not available") {
		t.Errorf("minimal sensors = %+v", m.Blocks)
	}
}

func TestPowerView(t *testing.T) {
	v := build(t, "power", "threadripper", Options{})
	if p := kvValue(v.Blocks, "*", "Profile"); !strings.HasPrefix(p, "balanced") {
		t.Errorf("profile = %q", p)
	}
	noEmptyValues(t, v)
	l := build(t, "power", "laptop", Options{})
	charge := false
	for _, g := range groups(l) {
		for _, b := range g.Blocks {
			if b.Type == "meter" && b.Label == "Charge" {
				charge = true
			}
		}
	}
	if !charge {
		t.Errorf("laptop battery group has no Charge meter: %+v", l.Blocks)
	}
	var peripherals *Block
	for i := range l.Blocks {
		if l.Blocks[i].Title == "Peripherals" {
			peripherals = &l.Blocks[i]
		}
	}
	if peripherals == nil || len(peripherals.Rows.([]Row)) != 1 {
		t.Errorf("peripherals = %+v", peripherals)
	}
	noEmptyValues(t, l)
	if gs := groups(build(t, "power", "minimal", Options{})); len(gs) != 0 {
		t.Errorf("minimal power groups = %+v", gs)
	}
}

// body son los bloques de una vista sin su cabecera.
func body(v *View) []Block {
	if len(v.Blocks) > 0 && v.Blocks[0].Type == "hero" {
		return v.Blocks[1:]
	}
	return v.Blocks
}
