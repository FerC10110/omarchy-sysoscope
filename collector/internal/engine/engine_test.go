package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/gpu"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

func fakeStatfs(string) (uint64, uint64, uint64, error) { return 1000, 400, 300, nil }

func newEngine(t *testing.T, env source.Env, focus string) *Engine {
	t.Helper()
	return New(env, Options{Interval: 2 * time.Second, Focus: focus, Statfs: fakeStatfs})
}

func runs(env source.Env) *source.FakeRunner { return env.Run.(*source.FakeRunner) }

func TestCadence(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	e := newEngine(t, env, "")
	for i := 0; i < 6; i++ {
		e.Tick()
	}
	r := runs(env)
	if n := r.Count(source.Key("nvidia-smi", gpu.SMIArgs...)); n != 2 {
		t.Errorf("nvidia-smi metrics = %d, want 2 (ticks 1 and 4)", n)
	}
	if n := r.Count("lsblk"); n != 1 {
		t.Errorf("lsblk = %d, want 1", n)
	}
	if n := r.Count("inxi"); n != 1 {
		t.Errorf("inxi = %d, want 1", n)
	}
	if n := r.Count("systemd-analyze"); n != 1 {
		t.Errorf("systemd-analyze = %d, want 1", n)
	}
	if n := r.Count("fwupdmgr"); n != 0 {
		t.Errorf("fwupdmgr = %d, want 0 without focus", n)
	}
}

func TestFocusGPUProcesses(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	e := newEngine(t, env, "")
	e.Tick()
	procs := source.Key("nvidia-smi", "--query-compute-apps=gpu_bus_id,pid,used_memory", "--format=csv,noheader,nounits")
	count := func() int {
		n := 0
		for _, c := range runs(env).Calls {
			if c == procs {
				n++
			}
		}
		return n
	}
	if count() != 0 {
		t.Fatal("processes queried without focus")
	}
	e.SetFocus("gpu")
	e.RefreshSlow()
	s := e.Tick()
	if count() != 1 || len(s.GPUs[0].Processes) != 2 {
		t.Fatalf("with focus gpu: %d calls, processes %+v", count(), s.GPUs[0].Processes)
	}
}

func TestStaticCache(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	e := newEngine(t, env, "")
	s1 := e.Tick()
	e.Close()
	if _, err := os.Stat(filepath.Join(env.Cache, "static.json")); err != nil {
		t.Fatal("no static cache written")
	}
	env2 := source.Fixture(t, "threadripper")
	env2.Cache, env2.State = env.Cache, env.State
	e2 := newEngine(t, env2, "")
	s2 := e2.Tick()
	if n := runs(env2).Count("inxi") + runs(env2).Count("systemd-analyze"); n != 0 {
		t.Fatalf("static commands re-run with the same boot_id: %v", runs(env2).Calls)
	}
	if s2.CPU.Model != s1.CPU.Model || len(s2.Memory.Modules) != 3 || s2.System.Board.Name != "ROG ZENITH EXTREME" || s2.GPUs[0].Card != "card1" {
		t.Fatalf("cached static lost data: %+v", s2.System.Board)
	}
	// otro boot_id invalida la caché
	os.WriteFile(filepath.Join(env2.Cache, "static.json"), []byte(`{"schema":1,"boot_id":"other"}`), 0o644)
	env3 := source.Fixture(t, "threadripper")
	env3.Cache, env3.State = env.Cache, env.State
	newEngine(t, env3, "").Tick()
	if runs(env3).Count("inxi") != 1 {
		t.Fatal("other boot_id must re-collect")
	}
}

func TestSnapshotValid(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	e := newEngine(t, env, "")
	e.Tick()
	s := e.Tick()
	if s.Schema != 1 || !s.Time.Equal(source.FixtureNow) {
		t.Fatalf("schema/time = %d %v", s.Schema, s.Time)
	}
	if _, err := json.Marshal(s); err != nil {
		t.Fatal(err)
	}
	for _, u := range s.Unavailable {
		if u.Reason == "" || u.Message == "" || u.What == "" {
			t.Errorf("incomplete unavailable %+v", u)
		}
	}
	if s.CPU.TempC == nil || *s.CPU.TempC != 28.875 {
		t.Errorf("cpu temp = %v", s.CPU.TempC)
	}
	for _, d := range s.Storage.Disks {
		if d.Name == "nvme0n1" && (d.TempC == nil || *d.TempC != 18.85) {
			t.Errorf("nvme0n1 temp = %v", d.TempC)
		}
		if d.Name == "sda" && (d.TempC == nil || *d.TempC != 36.85) {
			t.Errorf("sda temp (from SMART) = %v", d.TempC)
		}
	}
	if s.Storage.Filesystems[0].SizeB != 1000 || s.Self == nil || s.Self.Interval != 2 || s.Firmware != nil {
		t.Errorf("fs/self/firmware = %+v %+v %+v", s.Storage.Filesystems[0], s.Self, s.Firmware)
	}
	if len(e.Recent("mem.used_pct")) != 2 {
		t.Errorf("recent = %v", e.Recent("mem.used_pct"))
	}
}

func TestHistorySavedOnClose(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	e := newEngine(t, env, "")
	e.Tick()
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(env.State, "history.v1")); err != nil {
		t.Fatal("history not saved")
	}
}

func TestCorruptHistoryReported(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	os.WriteFile(filepath.Join(env.State, "history.v1"), []byte("garbage"), 0o644)
	s := newEngine(t, env, "").Tick()
	found := false
	for _, u := range s.Unavailable {
		if u.What == "history" {
			found = true
		}
	}
	if !found {
		t.Fatalf("corrupt history not reported: %+v", s.Unavailable)
	}
}

func TestMinimalMachine(t *testing.T) {
	env := source.Fixture(t, "minimal")
	e := newEngine(t, env, "")
	var last = e.Tick()
	for i := 0; i < 4; i++ {
		last = e.Tick()
	}
	if last.CPU == nil || last.Memory == nil || last.Memory.TotalB == 0 || len(last.Unavailable) == 0 {
		t.Fatalf("minimal snapshot = %+v", last)
	}
	if _, err := json.Marshal(last); err != nil {
		t.Fatal(err)
	}
}

func TestSetInterval(t *testing.T) {
	e := newEngine(t, source.Fixture(t, "minimal"), "")
	e.SetInterval(0)
	if e.Interval() != time.Second {
		t.Fatalf("interval = %v", e.Interval())
	}
	e.SetInterval(5 * time.Minute)
	if e.Interval() != 60*time.Second {
		t.Fatalf("interval = %v", e.Interval())
	}
}

func TestStreamUsesNvidiaLoop(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	env.State, env.Cache = t.TempDir(), t.TempDir()
	e := New(env, Options{Interval: 2 * time.Second, Stream: true, Statfs: fakeStatfs})
	defer e.Close()
	if e.gpuS.Loop != 6*time.Second {
		t.Fatalf("loop period = %v, want 3 ticks", e.gpuS.Loop)
	}
	e.SetInterval(time.Second)
	if e.gpuS.Loop != 3*time.Second {
		t.Fatalf("loop period after SetInterval = %v", e.gpuS.Loop)
	}
	if one := New(env, Options{Interval: 2 * time.Second, Statfs: fakeStatfs}); one.gpuS.Loop != 0 {
		t.Fatal("snapshot engines must poll nvidia-smi once, not loop")
	}
}

// withoutTools quita nvidia-smi y power-profiles-daemon del fixture.
func withoutTools(t *testing.T, env source.Env) source.Env {
	t.Helper()
	src := env.Run.(*source.FakeRunner).Dir
	dst := t.TempDir()
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	gone, _ := filepath.Glob(filepath.Join(dst, "nvidia-smi_*"))
	for _, f := range gone {
		os.Remove(f)
	}
	env.Run = &source.FakeRunner{Dir: dst}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(src), "dbus.json"))
	if err != nil {
		t.Fatal(err)
	}
	var bus map[string]map[string]any
	json.Unmarshal(raw, &bus)
	for k := range bus["system"] {
		if strings.HasPrefix(k, "net.hadess.PowerProfiles ") {
			delete(bus["system"], k)
		}
	}
	p := filepath.Join(t.TempDir(), "dbus.json")
	out, _ := json.Marshal(bus)
	os.WriteFile(p, out, 0o644)
	if env.Bus, err = source.LoadFakeBus(p); err != nil {
		t.Fatal(err)
	}
	return env
}

func TestUnavailableSurvivesBetweenSlowReads(t *testing.T) {
	env := withoutTools(t, source.Fixture(t, "threadripper"))
	e := newEngine(t, env, "")
	for tick := range 4 {
		got := map[string]bool{}
		for _, u := range e.Tick().Unavailable {
			got[u.What] = true
		}
		if !got["gpu.metrics"] || !got["power.profile"] {
			t.Fatalf("tick %d: unavailable = %v, want gpu.metrics and power.profile on every tick", tick, got)
		}
	}
}

func TestCacheHitDoesNotRerunPacman(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	newEngine(t, env, "").Tick()
	before := runs(env).Count("pacman")
	newEngine(t, env, "").Tick()
	if n := runs(env).Count("pacman"); n != before {
		t.Fatalf("pacman -Qqm ran again on a static-cache hit (%d → %d)", before, n)
	}
}

// static.json no guarda el hostname ni las series de los monitores; la
// muestra los tiene igual, con caché o sin él. Un static.json viejo que sí
// los tiene se descarta y se reescribe sin ellos.
func TestStaticCacheWithoutIdentifiers(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	s1 := newEngine(t, env, "").Tick()
	if s1.System.Hostname != "host" || s1.System.Monitors[0].Serial == "" {
		t.Fatalf("the fixture has no hostname or monitor serial: %q %+v", s1.System.Hostname, s1.System.Monitors)
	}
	serial := s1.System.Monitors[0].Serial
	p := filepath.Join(env.Cache, "static.json")
	clean := func() {
		t.Helper()
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range []string{`"host"`, serial} {
			if strings.Contains(string(b), x) {
				t.Fatalf("static.json has %s: %s", x, b)
			}
		}
	}
	clean()
	env2 := source.Fixture(t, "threadripper")
	env2.Cache, env2.State = env.Cache, env.State
	s2 := newEngine(t, env2, "").Tick()
	if runs(env2).Count("systemd-analyze") != 0 {
		t.Fatal("the cache was not used")
	}
	if s2.System.Hostname != "host" || s2.System.Monitors[0].Serial != serial {
		t.Fatalf("from the cache: hostname %q, monitors %+v", s2.System.Hostname, s2.System.Monitors)
	}
	// Un caché de antes (con hostname y series, sin la marca de privado).
	var old map[string]any
	b, _ := os.ReadFile(p)
	json.Unmarshal(b, &old)
	delete(old, "private")
	sys := old["system_static"].(map[string]any)
	sys["hostname"] = "OLD-HOST-SENTINEL"
	sys["monitors"].([]any)[0].(map[string]any)["serial"] = "OLD-SERIAL-SENTINEL"
	b, _ = json.Marshal(old)
	os.WriteFile(p, b, 0o644)
	env3 := source.Fixture(t, "threadripper")
	env3.Cache, env3.State = env.Cache, env.State
	s3 := newEngine(t, env3, "").Tick()
	if runs(env3).Count("systemd-analyze") != 1 {
		t.Fatal("an old cache must be read again")
	}
	b, _ = os.ReadFile(p)
	if strings.Contains(string(b), "SENTINEL") {
		t.Fatalf("the old static.json was not rewritten: %s", b)
	}
	clean()
	if s3.System.Hostname != "host" || s3.System.Monitors[0].Serial != serial {
		t.Fatalf("after the rewrite: %q %+v", s3.System.Hostname, s3.System.Monitors)
	}
}
