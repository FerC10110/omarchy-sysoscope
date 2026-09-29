package gpu

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

func fv(p *float64) float64 {
	if p == nil {
		return -1
	}
	return *p
}

func TestNvidiaThreadripper(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	r := &collect.Report{Lang: "en"}
	gpus := Static(env, r)
	if len(gpus) != 1 {
		t.Fatalf("gpus = %+v", gpus)
	}
	s := Sampler{Procs: true}
	s.Sample(env, gpus, r, true)
	g := gpus[0]
	if g.Card != "card1" || g.Name != "NVIDIA GeForce RTX 3080" || g.Vendor != "nvidia" || g.PCI != "0000:42:00.0" || g.Driver != "nvidia" {
		t.Errorf("identity = %+v", g)
	}
	if g.VendorID != "10de" || g.DeviceID != "2206" || g.DriverVersion != "610.57.04" || g.VBIOS != "94.02.42.00.2B" || g.Integrated || g.Suspended {
		t.Errorf("ids/driver = %+v", g)
	}
	want := model.PCIeLink{CurGTs: 2.5, MaxGTs: 16, CurGen: 1, MaxGen: 4, CurWidth: 16, MaxWidth: 16}
	if g.Link == nil || *g.Link != want {
		t.Errorf("link = %+v, want %+v (raw values, no judgment)", g.Link, want)
	}
	if fv(g.UsagePct) != 12 || g.VRAMUsedB == nil || *g.VRAMUsedB != 1231<<20 || *g.VRAMTotalB != 10240<<20 {
		t.Errorf("usage/vram = %v %v %v", fv(g.UsagePct), g.VRAMUsedB, g.VRAMTotalB)
	}
	if fv(g.TempC) != 39 || fv(g.PowerW) != 24.24 || fv(g.PowerLimitW) != 320 || fv(g.CoreMHz) != 210 || fv(g.MemMHz) != 405 || fv(g.FanPct) != 0 {
		t.Errorf("temp/power/clocks/fan = %v %v %v %v %v %v", fv(g.TempC), fv(g.PowerW), fv(g.PowerLimitW), fv(g.CoreMHz), fv(g.MemMHz), fv(g.FanPct))
	}
	if len(g.Processes) != 2 || g.Processes[0] != (model.GPUProc{PID: 613755, Name: "Hyprland", VRAMB: 206 << 20}) || g.Processes[1].Name != "firefox" {
		t.Errorf("processes = %+v", g.Processes)
	}
	if len(r.List) != 0 {
		t.Errorf("unavailable = %+v", r.List)
	}
}

func TestNvidiaSlowCadence(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	r := &collect.Report{Lang: "en"}
	gpus := Static(env, r)
	var s Sampler
	s.Sample(env, gpus, r, true)
	s.Sample(env, gpus, r, false)
	runs := env.Run.(*source.FakeRunner)
	if n := runs.Count("nvidia-smi"); n != 1 {
		t.Fatalf("nvidia-smi calls = %d, want 1 (only slow ticks)", n)
	}
	if fv(gpus[0].UsagePct) != 12 {
		t.Fatalf("fast tick must keep the last NVIDIA reading, got %v", fv(gpus[0].UsagePct))
	}
	if gpus[0].Processes != nil {
		t.Fatal("processes without focus")
	}
}

func TestOptimusSuspended(t *testing.T) {
	env := source.Fixture(t, "optimus")
	r := &collect.Report{Lang: "en"}
	gpus := Static(env, r)
	var s Sampler
	s.Sample(env, gpus, r, true)
	s.Sample(env, gpus, r, true)
	if n := env.Run.(*source.FakeRunner).Count("nvidia-smi"); n != 0 {
		t.Fatalf("nvidia-smi called %d times on a suspended dGPU", n)
	}
	if len(gpus) != 2 {
		t.Fatalf("gpus = %+v", gpus)
	}
	i, n := gpus[0], gpus[1]
	if !i.Integrated || i.Vendor != "intel" || fv(i.TempC) != 51 || i.Name != "Alder Lake-P GT2 [Iris Xe Graphics]" {
		t.Errorf("intel = %+v", i)
	}
	if !n.Suspended || n.UsagePct != nil || n.TempC != nil || n.Link != nil && n.Link.CurGTs != 0 {
		t.Errorf("nvidia suspended = %+v", n)
	}
	if n.Link == nil || n.Link.MaxGen != 3 {
		t.Errorf("static max link must stay: %+v", n.Link)
	}
	if b, _ := json.Marshal(n.Link); strings.Contains(string(b), "cur_") {
		t.Errorf("a suspended GPU's current link is unknown, not zero: %s", b)
	}
}

func TestNoNvidiaDriver(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	env.FS.Root = copyWithout(t, env.FS.Root, "proc/driver")
	r := &collect.Report{Lang: "en"}
	gpus := Static(env, r)
	var s Sampler
	s.Sample(env, gpus, r, true)
	if n := env.Run.(*source.FakeRunner).Count("nvidia-smi"); n != 0 {
		t.Fatalf("nvidia-smi calls = %d", n)
	}
	if gpus[0].UsagePct != nil {
		t.Fatal("invented usage")
	}
	if len(r.List) != 1 || r.List[0].What != "gpu.metrics" || r.List[0].Message != "requires the NVIDIA driver" {
		t.Fatalf("unavailable = %+v", r.List)
	}
}

func TestAMDAPU(t *testing.T) {
	env := source.Fixture(t, "amdapu")
	r := &collect.Report{Lang: "en"}
	gpus := Static(env, r)
	var s Sampler
	s.Sample(env, gpus, r, false)
	g := gpus[0]
	if g.Vendor != "amd" || !g.Integrated || g.Name != "Phoenix1" || g.VBIOS != "113-PHXGENERIC-001" {
		t.Errorf("identity = %+v", g)
	}
	if fv(g.UsagePct) != 9 || *g.VRAMUsedB != 402653184 || *g.VRAMTotalB != 536870912 || *g.GTTUsedB != 1073741824 || *g.GTTTotalB != 16777216000 {
		t.Errorf("usage/mem = %+v", g)
	}
	if fv(g.TempC) != 42 || fv(g.PowerW) != 8 {
		t.Errorf("temp/power = %v %v", fv(g.TempC), fv(g.PowerW))
	}
}

func TestNvidiaNA(t *testing.T) {
	g := model.GPU{PCI: "0000:42:00.0"}
	applySMI(&g, parseSMI("00000000:42:00.0, NVIDIA GeForce RTX 3080, 610.57.04, 94.02.42.00.2B, 12, 1231, 10240, 39, [N/A], [N/A], 210, 405, [N/A]\n")[0])
	if g.PowerW != nil || g.PowerLimitW != nil || g.FanPct != nil || fv(g.UsagePct) != 12 || fv(g.TempC) != 39 {
		t.Fatalf("N/A handling = %+v", g)
	}
}

func TestGen(t *testing.T) {
	for gts, gen := range map[float64]int{2.5: 1, 5: 2, 8: 3, 16: 4, 32: 5, 64: 6, 3: 0} {
		if got := genOf(gts); got != gen {
			t.Errorf("genOf(%v) = %d, want %d", gts, got, gen)
		}
	}
}

func TestNvidiaLoopOnAlwaysOnGPU(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	env.FS.Root = copyWithout(t, env.FS.Root, "nothing")
	writeFile(t, env.FS.Root, "sys/class/drm/card1/device/power/control", "on")
	r := &collect.Report{Lang: "en"}
	gpus := Static(env, r)
	s := Sampler{Loop: 6 * time.Second}
	defer s.Close()
	s.Sample(env, gpus, r, true)
	deadline := time.Now().Add(2 * time.Second)
	for fv(gpus[0].UsagePct) != 33 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		s.Sample(env, gpus, r, false)
	}
	if fv(gpus[0].UsagePct) != 33 || fv(gpus[0].CoreMHz) != 1500 || fv(gpus[0].PowerW) != 30.5 {
		t.Fatalf("loop reading = usage %v core %v power %v", fv(gpus[0].UsagePct), fv(gpus[0].CoreMHz), fv(gpus[0].PowerW))
	}
	fr := env.Run.(*source.FakeRunner)
	oneShot := source.Key("nvidia-smi", SMIArgs...)
	loop := source.Key("nvidia-smi", append(append([]string{}, SMIArgs...), "-lms", "6000")...)
	before := count(fr.Calls, oneShot)
	s.Sample(env, gpus, r, true)
	if count(fr.Calls, loop) != 1 || count(fr.Calls, oneShot) != before {
		t.Fatalf("with loop data a slow tick must not start nvidia-smi again: %v", fr.Calls)
	}
}

func count(calls []string, k string) int {
	n := 0
	for _, c := range calls {
		if c == k {
			n++
		}
	}
	return n
}

// stuckRunner arranca un nvidia-smi en bucle que nunca escribe nada.
type stuckRunner struct {
	*source.FakeRunner
	streams int
	closed  atomic.Int32
}

func (s *stuckRunner) Stream(ctx context.Context, name string, args ...string) (io.ReadCloser, error) {
	s.streams++
	return &stuckReader{ctx: ctx, closed: &s.closed}, nil
}

type stuckReader struct {
	ctx    context.Context
	closed *atomic.Int32
}

func (r *stuckReader) Read([]byte) (int, error) { <-r.ctx.Done(); return 0, io.EOF }
func (r *stuckReader) Close() error             { r.closed.Add(1); return nil }

func TestNvidiaStalledLoopFallsBackAndRelaunches(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	env.FS.Root = copyWithout(t, env.FS.Root, "nothing")
	writeFile(t, env.FS.Root, "sys/class/drm/card1/device/power/control", "on")
	now := source.FixtureNow
	env.Now = func() time.Time { return now }
	run := &stuckRunner{FakeRunner: env.Run.(*source.FakeRunner)}
	env.Run = run
	gpus := Static(env, &collect.Report{Lang: "en"})
	s := Sampler{Loop: 6 * time.Second}
	defer s.Close()

	r := &collect.Report{Lang: "en"}
	s.Sample(env, gpus, r, true)
	if gpus[0].UsagePct == nil {
		t.Fatal("before the loop sends anything, a slow tick must poll nvidia-smi once")
	}
	now = now.Add(20 * time.Second)
	s.Sample(env, gpus, &collect.Report{Lang: "en"}, true)
	deadline := time.Now().Add(2 * time.Second)
	for run.closed.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if run.closed.Load() != 1 {
		t.Fatal("a loop silent for 3 periods must be killed")
	}
	if gpus[0].UsagePct == nil {
		t.Fatal("while the loop is dead, slow ticks must poll nvidia-smi once")
	}
	now = now.Add(2 * time.Minute)
	s.Sample(env, gpus, &collect.Report{Lang: "en"}, true)
	if run.streams != 2 {
		t.Fatalf("a dead loop must be relaunched after a minute, streams = %d", run.streams)
	}
}

func TestNvidiaNoLoopWithRuntimePM(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	env.FS.Root = copyWithout(t, env.FS.Root, "nothing")
	writeFile(t, env.FS.Root, "sys/class/drm/card1/device/power/control", "auto")
	r := &collect.Report{Lang: "en"}
	gpus := Static(env, r)
	s := Sampler{Loop: 6 * time.Second}
	defer s.Close()
	s.Sample(env, gpus, r, true)
	s.Sample(env, gpus, r, false)
	oneShot := source.Key("nvidia-smi", SMIArgs...)
	calls := env.Run.(*source.FakeRunner).Calls
	if len(calls) != 1 || calls[0] != oneShot {
		t.Fatalf("a runtime-PM GPU must be polled only on slow ticks: %v", calls)
	}
}
