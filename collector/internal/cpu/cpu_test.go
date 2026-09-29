package cpu

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

func capability(c *model.CPU, name string) (model.Capability, bool) {
	for _, k := range c.Capabilities {
		if k.Name == name {
			return k, true
		}
	}
	return model.Capability{}, false
}

func vuln(c *model.CPU, name string) string {
	for _, v := range c.Vulnerabilities {
		if v.Name == name {
			return v.State
		}
	}
	return ""
}

func TestStaticThreadripper(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	r := &collect.Report{Lang: "en"}
	c := Static(env, r)
	if c.Model != "AMD Ryzen Threadripper 1920X 12-Core Processor" || c.Vendor != "AMD" || c.Arch != "x86_64" {
		t.Errorf("model/vendor/arch = %q %q %q", c.Model, c.Vendor, c.Arch)
	}
	if c.Threads != 24 || c.Cores != 12 || c.Sockets != 1 {
		t.Errorf("threads/cores/sockets = %d/%d/%d", c.Threads, c.Cores, c.Sockets)
	}
	if c.MaxMHz == nil || *c.MaxMHz != 4200 || c.MinMHz == nil || *c.MinMHz != 2200 {
		t.Errorf("min/max = %v/%v", c.MinMHz, c.MaxMHz)
	}
	if c.Governor != "schedutil" || c.Driver != "acpi-cpufreq" || c.Microcode != "0x8001137" {
		t.Errorf("gov/driver/ucode = %q %q %q", c.Governor, c.Driver, c.Microcode)
	}
	if c.Boost == nil || !*c.Boost {
		t.Errorf("boost = %v", c.Boost)
	}
	wantCaches := []model.Cache{
		{Level: 1, Type: "Data", SizeB: 32 << 10, Instances: 12},
		{Level: 1, Type: "Instruction", SizeB: 64 << 10, Instances: 12},
		{Level: 2, Type: "Unified", SizeB: 512 << 10, Instances: 12},
		{Level: 3, Type: "Unified", SizeB: 8 << 20, Instances: 4},
	}
	if !reflect.DeepEqual(c.Caches, wantCaches) {
		t.Errorf("caches = %+v", c.Caches)
	}
	if len(c.Topology) != 24 {
		t.Fatalf("topology = %d", len(c.Topology))
	}
	if c.Topology[0].L3 != 0 || !reflect.DeepEqual(c.Topology[0].Sibling, []int{0, 12}) || c.Topology[3].L3 != 1 || c.Topology[12].Core != 0 {
		t.Errorf("topology = %+v %+v", c.Topology[0], c.Topology[3])
	}
	for name, want := range map[string]bool{"AMD-V": true, "VT-x": false, "AVX2": true, "AVX-512F": false, "SHA-NI": true, "SMEP": true, "NPT": true} {
		k, ok := capability(c, name)
		if !ok || k.Supported != want {
			t.Errorf("capability %s = %+v %v, want %v", name, k, ok, want)
		}
	}
	if k, _ := capability(c, "AMD-V"); k.Group != "virtualization" {
		t.Errorf("AMD-V group = %q", k.Group)
	}
	if _, ok := capability(c, "NEON"); ok {
		t.Error("x86 must not list NEON")
	}
	for name, want := range map[string]string{"retbleed": "mitigated", "meltdown": "not_affected", "mds": "vulnerable", "spectre_v2": "mitigated"} {
		if got := vuln(c, name); got != want {
			t.Errorf("vuln %s = %q, want %q", name, got, want)
		}
	}
	if len(r.List) != 0 {
		t.Errorf("unexpected unavailable: %+v", r.List)
	}
}

// swapStat copia el fixture a un directorio temporal y cambia /proc/stat.
func swapStat(t *testing.T, env *source.Env, statFile string) {
	t.Helper()
	dst := t.TempDir()
	if err := os.CopyFS(dst, os.DirFS(env.FS.Root)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(statFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "proc/stat"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	env.FS.Root = dst
}

func TestUsageNeedsTwoSamples(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	r := &collect.Report{Lang: "en"}
	c := Static(env, r)
	var s Sampler
	sens := &model.Sensors{Chips: []model.Chip{{Name: "k10temp", Kind: "cpu", Temps: []model.Reading{{Label: "Tctl", Value: 55.875}, {Label: "Tdie", Value: 28.875}}}}}
	s.Sample(env, c, sens, r)
	if c.UsagePct != nil || c.PerCPUPct != nil {
		t.Fatalf("first sample usage = %v", c.UsagePct)
	}
	if c.TempC == nil || *c.TempC != 28.875 || c.TempSensor != "k10temp Tdie" {
		t.Errorf("temp = %v %q", c.TempC, c.TempSensor)
	}
	if c.Load[0] <= 0 {
		t.Errorf("load = %v", c.Load)
	}
	if c.UptimeS <= 0 || c.PSI == nil {
		t.Errorf("uptime/psi = %v %v", c.UptimeS, c.PSI)
	}
	if len(c.FreqMHz) != 24 || c.FreqMHz[0] != 1985.154 || c.AvgMHz == nil {
		t.Errorf("freq = %v avg %v", c.FreqMHz, c.AvgMHz)
	}
	swapStat(t, &env, filepath.Join(source.FixturesDir(t), "threadripper/stat.2"))
	s.Sample(env, c, sens, r)
	if c.UsagePct == nil || *c.UsagePct != 25 {
		t.Fatalf("usage = %v, want 25", c.UsagePct)
	}
	if len(c.PerCPUPct) != 24 || c.PerCPUPct[0] != 25 {
		t.Fatalf("per cpu = %v", c.PerCPUPct)
	}
}

func TestMinimal(t *testing.T) {
	env := source.Fixture(t, "minimal")
	r := &collect.Report{Lang: "en"}
	c := Static(env, r)
	var s Sampler
	s.Sample(env, c, nil, r)
	s.Sample(env, c, nil, r)
	if c.Model != "Test CPU" || c.Threads != 2 || c.Vendor != "Intel" {
		t.Errorf("minimal cpu = %q %d %q", c.Model, c.Threads, c.Vendor)
	}
	if c.MaxMHz != nil || c.TempC != nil || c.Boost != nil {
		t.Errorf("invented values: max %v temp %v boost %v", c.MaxMHz, c.TempC, c.Boost)
	}
	if c.UsagePct != nil {
		t.Errorf("usage with no elapsed ticks = %v, want nil", *c.UsagePct)
	}
	found := map[string]bool{}
	for _, u := range r.List {
		found[u.What] = true
	}
	for _, w := range []string{"cpu.freq", "cpu.temp"} {
		if !found[w] {
			t.Errorf("missing unavailable %s in %+v", w, r.List)
		}
	}
}

func TestSkipFreqKeepsLast(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	r := &collect.Report{Lang: "en"}
	c := Static(env, r)
	var s Sampler
	s.Sample(env, c, nil, r)
	if len(c.FreqMHz) != 24 {
		t.Fatalf("freq = %v", c.FreqMHz)
	}
	env.FS.Root = t.TempDir() // si leyera, no encontraría nada
	s.SkipFreq = true
	r = &collect.Report{Lang: "en"}
	s.Sample(env, c, nil, r)
	if len(c.FreqMHz) != 24 || c.AvgMHz == nil {
		t.Fatalf("skipped freq must keep the last reading: %v", c.FreqMHz)
	}
	for _, u := range r.List {
		if u.What == "cpu.freq" {
			t.Fatalf("skipped freq must not be reported: %+v", r.List)
		}
	}
}
