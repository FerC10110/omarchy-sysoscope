package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/maint"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

var (
	errKey  = source.Key("journalctl", maint.JournalErrArgs...)
	dumpKey = source.Key("coredumpctl", maint.CoredumpArgs...)
	oomdKey = source.Key("journalctl", maint.OomdArgs...)
	kernKey = source.Key("journalctl", maint.KernelOOMArgs...)
)

// healthCalls: errores, coredumpctl, systemd-oomd y el journal del kernel.
func healthCalls(r *source.FakeRunner) [4]int {
	return [4]int{r.Count(errKey), r.Count(dumpKey), r.Count(oomdKey), r.Count(kernKey)}
}

func TestHealthClosed(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	s := maintEngine(t, env, "", false).Tick()
	if s.Memory.OOMKills == nil || *s.Memory.OOMKills != 0 {
		t.Fatalf("oom_kills = %v", s.Memory.OOMKills)
	}
	if s.Maintenance.Health != nil || healthCalls(runs(env)) != [4]int{} {
		t.Fatalf("health read with no section open: %+v, %v", s.Maintenance.Health, healthCalls(runs(env)))
	}
}

func TestHealthMaintenanceCadence(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	now := source.FixtureNow
	env.Now = func() time.Time { return now }
	e := maintEngine(t, env, "maintenance", false)
	h := e.Tick().Maintenance.Health
	if h == nil || h.Errors == nil || h.Errors.Count != 32 || h.Errors.Sources[0] != (model.ErrorSource{Name: "kernel", Count: 23}) ||
		h.Errors.Recent[0].Message != "message 32" || h.Crashes == nil || h.Crashes.Total != 2 ||
		h.Crashes.List[0].PID != 1310465 || h.Crashes.List[0].User != "user" || h.Crashes.List[0].SignalName != "SIGILL" ||
		h.OOM == nil || h.OOM.Oomd != 0 || len(h.OOM.List) != 0 {
		t.Fatalf("health = %+v", h)
	}
	r := runs(env)
	check := func(step string, want [4]int) {
		t.Helper()
		if got := healthCalls(r); got != want {
			t.Fatalf("%s: calls = %v, want %v", step, got, want)
		}
	}
	check("first read (the kernel's only with oom_kill > 0)", [4]int{1, 1, 1, 0})
	now = now.Add(2 * time.Second)
	e.Tick()
	check("after 2 s", [4]int{1, 1, 1, 0})
	now = now.Add(healthEvery)
	e.Tick()
	check("after 30 s", [4]int{2, 2, 2, 0})
	// Fuera de la sección no se lee y queda lo último.
	e.SetFocus("")
	now = now.Add(healthEvery)
	if s := e.Tick(); s.Maintenance.Health == nil || s.Maintenance.Health.Errors.Count != 32 {
		t.Fatalf("closed: %+v", s.Maintenance.Health)
	}
	check("closed", [4]int{2, 2, 2, 0})
	// Al volver, pasó su período: se relee; r fuerza.
	e.SetFocus("maintenance")
	e.Tick()
	check("entering after 30 s", [4]int{3, 3, 3, 0})
	e.RefreshMaintenance()
	e.Tick()
	check("refresh", [4]int{4, 4, 4, 0})
}

func TestHealthMemoryOnlyOOM(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	now := source.FixtureNow
	env.Now = func() time.Time { return now }
	e := maintEngine(t, env, "memory", false)
	s := e.Tick()
	h := s.Maintenance.Health
	if h == nil || h.OOM == nil || h.Errors != nil || h.Crashes != nil {
		t.Fatalf("memory = %+v", h)
	}
	r := runs(env)
	if got := healthCalls(r); got != [4]int{0, 0, 1, 0} {
		t.Fatalf("calls = %v, want only systemd-oomd", got)
	}
	if r.Count(source.Key("journalctl", "--disk-usage")) != 0 || s.Maintenance.Caches != nil || s.Maintenance.Snapshots != nil {
		t.Fatal("Memory must not read Maintenance's caches or snapshots")
	}
	now = now.Add(2 * time.Second)
	e.Tick()
	now = now.Add(healthEvery)
	e.Tick()
	if got := healthCalls(r); got != [4]int{0, 0, 2, 0} {
		t.Fatalf("after 30 s = %v", got)
	}
}

// Review Focus 5: con muertes se lee el kernel, y una muerte nueva se lee
// en la muestra siguiente.
func TestHealthKernelOOM(t *testing.T) {
	env := source.FixtureCopy(t, "threadripper")
	dir := filepath.Dir(env.FS.Root)
	write := func(rel, s string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("root/proc/vmstat", "pgfault 1\noom_kill 2\n")
	write("cmd/"+kernKey+".txt",
		`{"MESSAGE":"Out of memory: Killed process 48213 (Web Content) total-vm:11534336kB, anon-rss:6291456kB, file-rss:0kB, shmem-rss:0kB, UID:1000 pgtables:14336kB oom_score_adj:167","__REALTIME_TIMESTAMP":"1790160000000000"}`+"\n"+
			`{"MESSAGE":"Memory cgroup out of memory: Killed process 5120 (stress-ng) total-vm:2097152kB, anon-rss:1048576kB, file-rss:0kB, shmem-rss:0kB, UID:1000 pgtables:2100kB oom_score_adj:0","__REALTIME_TIMESTAMP":"1790161000000000"}`+"\n")
	now := source.FixtureNow
	env.Now = func() time.Time { return now }
	e := maintEngine(t, env, "memory", false)
	s := e.Tick()
	l := s.Maintenance.Health.OOM
	if *s.Memory.OOMKills != 2 || l == nil || len(l.List) != 2 || l.List[0] != (model.OOMKill{Time: time.UnixMicro(1790161000000000).UTC(), Program: "stress-ng", By: "kernel"}) {
		t.Fatalf("kills = %v, %+v", *s.Memory.OOMKills, l)
	}
	r := runs(env)
	if n := r.Count(kernKey); n != 1 {
		t.Fatalf("kernel = %d", n)
	}
	// Una muerte nueva con la última lectura de hace 5 s o más se lee en la
	// muestra siguiente.
	write("root/proc/vmstat", "pgfault 1\noom_kill 3\n")
	now = now.Add(5 * time.Second)
	e.Tick()
	if n := r.Count(kernKey); n != 2 {
		t.Fatalf("a new kill must be read on the next sample: kernel = %d", n)
	}
	now = now.Add(2 * time.Second)
	e.Tick()
	if n := r.Count(kernKey); n != 2 {
		t.Fatalf("nothing new: kernel = %d", n)
	}
	// M1: un bucle de muertes (el contador cambia en cada muestra) lee a lo
	// sumo cada 5 s, no en cada muestra.
	write("root/proc/vmstat", "pgfault 1\noom_kill 4\n")
	now = now.Add(2 * time.Second) // 4 s desde la última lectura
	e.Tick()
	if n := r.Count(kernKey); n != 2 {
		t.Fatalf("a kill 4 s after the last read must wait: kernel = %d", n)
	}
	write("root/proc/vmstat", "pgfault 1\noom_kill 5\n")
	now = now.Add(time.Second) // 5 s
	e.Tick()
	if n := r.Count(kernKey); n != 3 {
		t.Fatalf("5 s after the last read the kill is read: kernel = %d", n)
	}
	write("root/proc/vmstat", "pgfault 1\noom_kill 6\n")
	now = now.Add(2 * time.Second)
	e.Tick()
	if n := r.Count(kernKey); n != 3 {
		t.Fatalf("kill loop: kernel = %d", n)
	}
}

// M1 (D): una muestra sin oom_kill (OOMKills nil) no es un cambio del
// contador: no dispara otra lectura ni pierde lo visto.
func TestHealthNilKillsNotAChange(t *testing.T) {
	env := source.FixtureCopy(t, "threadripper")
	dir := filepath.Dir(env.FS.Root)
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "root/proc/vmstat"), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("pgfault 1\noom_kill 2\n")
	now := source.FixtureNow
	env.Now = func() time.Time { return now }
	e := maintEngine(t, env, "memory", false)
	e.Tick()
	r := runs(env)
	if got := healthCalls(r); got != [4]int{0, 0, 1, 1} {
		t.Fatalf("first = %v", got)
	}
	write("pgfault 1\n")
	now = now.Add(6 * time.Second)
	if s := e.Tick(); s.Memory.OOMKills != nil {
		t.Fatalf("oom_kills = %v", *s.Memory.OOMKills)
	}
	if got := healthCalls(r); got != [4]int{0, 0, 1, 1} {
		t.Fatalf("a sample without oom_kill must not read: %v", got)
	}
	write("pgfault 1\noom_kill 2\n")
	now = now.Add(2 * time.Second)
	e.Tick()
	if got := healthCalls(r); got != [4]int{0, 0, 1, 1} {
		t.Fatalf("same count after a missing sample must not read: %v", got)
	}
}

// Review Focus 5: r en Memoria lee los OOM y deja pendiente lo de
// Mantenimiento.
func TestHealthRefreshInMemoryKeepsMaintenance(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	now := source.FixtureNow
	env.Now = func() time.Time { return now }
	e := maintEngine(t, env, "maintenance", false)
	e.Tick()
	r := runs(env)
	du := source.Key("journalctl", "--disk-usage")
	e.SetFocus("memory")
	now = now.Add(2 * time.Second)
	e.RefreshMaintenance()
	e.Tick()
	if got := healthCalls(r); got != [4]int{1, 1, 2, 0} || r.Count(du) != 1 {
		t.Fatalf("memory refresh: calls %v, disk-usage %d", got, r.Count(du))
	}
	e.SetFocus("maintenance")
	now = now.Add(2 * time.Second)
	e.Tick()
	if got := healthCalls(r); got != [4]int{2, 2, 3, 0} || r.Count(du) != 2 {
		t.Fatalf("the refresh asked on Memory must still apply to Maintenance: calls %v, disk-usage %d", got, r.Count(du))
	}
}
