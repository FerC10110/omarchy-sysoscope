package mem

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

func TestReadThreadripper(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	r := &collect.Report{Lang: "en"}
	m := Read(env, r)
	const k = 1024
	if m.TotalB != 49287176*k || m.AvailableB != 31072364*k || m.FreeB != 20005088*k {
		t.Errorf("total/avail/free = %d/%d/%d", m.TotalB, m.AvailableB, m.FreeB)
	}
	wantUsed := uint64(49287176-20005088-3332-13479920-1246336) * k
	if m.UsedB != wantUsed {
		t.Errorf("used = %d, want %d", m.UsedB, wantUsed)
	}
	if m.CacheB != (3332+13479920+1246336)*k || m.DirtyB != 1256*k || m.ShmemB != 874968*k {
		t.Errorf("cache/dirty/shmem = %d/%d/%d", m.CacheB, m.DirtyB, m.ShmemB)
	}
	if want := float64(wantUsed) / float64(m.TotalB) * 100; m.UsedPct != want {
		t.Errorf("used pct = %v, want %v", m.UsedPct, want)
	}
	if m.SwapTotalB != 98575120*k || m.SwapUsedB != 0 {
		t.Errorf("swap = %d/%d", m.SwapTotalB, m.SwapUsedB)
	}
	if len(m.Swaps) != 2 {
		t.Fatalf("swaps = %+v", m.Swaps)
	}
	f, z := m.Swaps[0], m.Swaps[1]
	if f.Name != "/swap/swapfile" || f.Kind != "file" || f.SizeB != 49287956*k || f.ZramRatio != nil {
		t.Errorf("swapfile = %+v", f)
	}
	if z.Name != "/dev/zram0" || z.Kind != "zram" || z.Priority != 100 || z.ZramAlgo != "zstd" || z.ZramRatio == nil || *z.ZramRatio != 2 {
		t.Errorf("zram = %+v", z)
	}
	if m.PSI == nil {
		t.Error("psi missing")
	}
}

func TestModulesInxi(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	r := &collect.Report{Lang: "en"}
	m := Read(env, r)
	Modules(env, r, m)
	if len(m.Modules) != 3 {
		t.Fatalf("modules = %+v", m.Modules)
	}
	d := m.Modules[0]
	if d.Slot != "Channel-A DIMM 1" || d.Type != "DDR4" || d.SizeB != 16<<30 || d.SpecMTs == nil || *d.SpecMTs != 2133 || d.ActualMTs == nil || *d.ActualMTs != 1067 {
		t.Errorf("module 0 = %+v", d)
	}
	if m.Modules[2].Slot != "Channel-C DIMM 1" {
		t.Errorf("module 2 slot = %q", m.Modules[2].Slot)
	}
	if m.Slots != 8 || m.CapacityB != 512<<30 || m.ECC != "None" {
		t.Errorf("slots/capacity/ecc = %d/%d/%q", m.Slots, m.CapacityB, m.ECC)
	}
	if len(r.List) != 0 {
		t.Errorf("unavailable = %+v", r.List)
	}
}

func TestModulesSimpleSpeed(t *testing.T) {
	m := parseInxi("  Array-1: capacity: 64 GiB slots: 2 modules: 1 EC: None\n  Device-1: DIMM A type: LPDDR5 size: 8 GiB speed: 6400 MT/s\n  Device-2: Controller0-ChannelA type: DDR5 size: 512 MiB speed: 4800 MT/s\n")
	if len(m.Modules) != 2 || m.Modules[0].Slot != "DIMM A" || *m.Modules[0].SpecMTs != 6400 || m.Modules[0].ActualMTs != nil || m.Modules[1].SizeB != 512<<20 {
		t.Fatalf("modules = %+v", m.Modules)
	}
}

func TestModulesNoInxi(t *testing.T) {
	env := source.Fixture(t, "minimal")
	r := &collect.Report{Lang: "en"}
	m := Read(env, r)
	Modules(env, r, m)
	if len(m.Modules) != 0 || m.Slots != 0 {
		t.Fatalf("modules = %+v", m.Modules)
	}
	if len(r.List) != 1 || r.List[0].What != "memory.modules" || r.List[0].Reason != "missing_tool" {
		t.Fatalf("unavailable = %+v", r.List)
	}
	if m.TotalB != 8000000*1024 || len(m.Swaps) != 0 {
		t.Errorf("minimal mem = %+v", m)
	}
}

// Review Focus 4: sin archivo, sin el campo (kernel viejo) o con un campo
// que empieza igual.
func TestOOMKills(t *testing.T) {
	if n := OOMKills(source.Fixture(t, "threadripper")); n == nil || *n != 0 {
		t.Fatalf("threadripper = %v", n)
	}
	m := Read(source.Fixture(t, "threadripper"), &collect.Report{Lang: "en"})
	if m.OOMKills == nil || *m.OOMKills != 0 {
		t.Fatalf("Read must fill OOMKills: %v", m.OOMKills)
	}
	dir := t.TempDir()
	env := source.Env{FS: source.FS{Root: dir}}
	if n := OOMKills(env); n != nil {
		t.Errorf("without /proc/vmstat = %d", *n)
	}
	if err := os.MkdirAll(filepath.Join(dir, "proc"), 0o755); err != nil {
		t.Fatal(err)
	}
	for body, want := range map[string]int64{
		"nr_free_pages 1\noom_kill 3\npgfault 9\n": 3,
		"oom_kill 7":                      7,
		"nr_free_pages 1\noom_kill_x 5\n": -1,
		"nr_free_pages 1\n":               -1,
		"oom_kill nope\n":                 -1,
	} {
		if err := os.WriteFile(filepath.Join(dir, "proc/vmstat"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		n := OOMKills(env)
		if (want < 0) != (n == nil) || (n != nil && int64(*n) != want) {
			t.Errorf("%q = %v, want %d", body, n, want)
		}
	}
}
