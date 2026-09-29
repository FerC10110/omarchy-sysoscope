package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/maint"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

const testDB = "/tmp/checkup-db-test"

// maintEngine es un engine sobre un Env de fixture con los tamaños de su
// dirsizes.json (nada recorre directorios).
func maintEngine(t *testing.T, env source.Env, focus string, stream bool) *Engine {
	t.Helper()
	sizes, err := maint.LoadDirSizes(filepath.Join(filepath.Dir(env.FS.Root), maint.SizesFile))
	if err != nil {
		t.Fatal(err)
	}
	return New(env, Options{Interval: 2 * time.Second, Focus: focus, Stream: stream, Statfs: fakeStatfs,
		DirSize: sizes, CheckupDB: testDB})
}

func bus(env source.Env) *source.FakeBus { return env.Bus.(*source.FakeBus) }

// Con la sección cerrada: reinicio y pacman.log sí; cachés, snapshots y
// actualizaciones no.
func TestMaintenanceClosed(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	s := maintEngine(t, env, "", false).Tick()
	m := s.Maintenance
	if m == nil || m.Reboot == nil || m.Upgrade == nil {
		t.Fatalf("maintenance = %+v", m)
	}
	rb := m.Reboot
	if rb.Kernel != "7.2.5-3-omarchy" || rb.KernelGone || rb.Marker || rb.Hyprland == nil || *rb.Hyprland ||
		rb.NVIDIALoaded != "610.57.04" || rb.NVIDIAInstalled != "610.57.04" ||
		!reflect.DeepEqual(rb.Installed, []string{"7.2.3-arch1-3", "7.2.5-3-omarchy"}) {
		t.Errorf("reboot = %+v", rb)
	}
	up := m.Upgrade
	if !up.LastFull.Equal(time.Date(2026, 9, 22, 23, 41, 32, 0, time.UTC)) || up.FullPackages != 2 ||
		!up.LastChange.Equal(time.Date(2026, 9, 22, 23, 42, 25, 0, time.UTC)) {
		t.Errorf("upgrade = %+v", up)
	}
	if m.Caches != nil || m.Snapshots != nil || m.Updates != nil || !m.CachesAt.IsZero() {
		t.Errorf("read without the section open: %+v", m)
	}
	if runs(env).Count("journalctl") != 0 || runs(env).Count("checkupdates") != 0 || bus(env).Called(maint.SnapperDest) != 0 {
		t.Error("commands or snapperd without the section open")
	}
}

func TestMaintenanceFocusCadence(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	now := source.FixtureNow
	env.Now = func() time.Time { return now }
	e := maintEngine(t, env, "maintenance", false)
	m := e.Tick().Maintenance
	var ids []string
	for _, c := range m.Caches {
		ids = append(ids, c.ID)
	}
	if !reflect.DeepEqual(ids, []string{"pacman", "yay", "journal", "coredump"}) || m.Caches[0].SizeB != 6764573491 ||
		!m.Caches[0].Partial || m.Caches[2].SizeB != 365009306 || !m.CachesAt.Equal(now) {
		t.Fatalf("caches = %+v at %v", m.Caches, m.CachesAt)
	}
	if sn := m.Snapshots; sn == nil || sn.User != "user" || len(sn.Configs) != 1 || sn.Configs[0].Allowed {
		t.Fatalf("snapshots = %+v", m.Snapshots)
	}
	if bus(env).Called(maint.SnapperDest) != 0 {
		t.Fatal("snapperd asked without permission")
	}
	r := runs(env)
	diskUsage := source.Key("journalctl", "--disk-usage") // la salud también corre journalctl
	now = now.Add(2 * time.Second)
	e.Tick()
	if n := r.Count(diskUsage); n != 1 {
		t.Fatalf("journalctl = %d after 2 s, want 1 (every 10 min)", n)
	}
	now = now.Add(cachesEvery)
	e.Tick()
	if n := r.Count(diskUsage); n != 2 {
		t.Fatalf("journalctl = %d after 10 min, want 2", n)
	}
	// Fuera de la sección no se mide y queda lo último leído.
	e.SetFocus("")
	now = now.Add(cachesEvery)
	if m := e.Tick().Maintenance; r.Count(diskUsage) != 2 || len(m.Caches) != 4 || m.Snapshots == nil {
		t.Fatalf("closed: journalctl %d, %+v", r.Count(diskUsage), m)
	}
	// Al volver, lo que tiene más de su período se relee; r fuerza.
	e.SetFocus("maintenance")
	e.Tick()
	if n := r.Count(diskUsage); n != 3 {
		t.Fatalf("journalctl = %d on entering after 10 min, want 3", n)
	}
	e.RefreshMaintenance()
	e.Tick()
	if n := r.Count(diskUsage); n != 4 {
		t.Fatalf("journalctl = %d after refresh, want 4", n)
	}
}

// allowSnapshots deja ver los snapshots de root al usuario del fixture y le
// da al FakeBus dos, sobre una copia del fixture.
func allowSnapshots(t *testing.T, env source.Env) (source.Env, *source.FakeBus) {
	t.Helper()
	cfg := filepath.Join(env.FS.Root, "etc/snapper/configs/root")
	if err := os.WriteFile(cfg, []byte("SUBVOLUME=\"/\"\nALLOW_USERS=\"user\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(filepath.Dir(env.FS.Root), "dbus.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var b map[string]map[string]any
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	b["system"]["call org.opensuse.Snapper /org/opensuse/Snapper org.opensuse.Snapper.ListSnapshots root"] = []any{[]any{
		[]any{0, 0, 0, -1, 0, "current", "", map[string]any{}},
		[]any{1, 1, 0, 1758580000, 0, "omarchy 4.0.3", "number", map[string]any{}},
		[]any{2, 2, 1, 1758580060, 0, "omarchy 4.0.3", "number", map[string]any{}},
	}}
	out, _ := json.Marshal(b)
	if err := os.WriteFile(p, out, 0o644); err != nil {
		t.Fatal(err)
	}
	fb, err := source.LoadFakeBus(p)
	if err != nil {
		t.Fatal(err)
	}
	env.Bus = fb
	return env, fb
}

// Snapshots: al entrar y con refresh, nunca solos.
func TestMaintenanceSnapshotsOnEnterAndRefresh(t *testing.T) {
	env, fb := allowSnapshots(t, source.FixtureCopy(t, "threadripper"))
	e := maintEngine(t, env, "maintenance", false)
	c := e.Tick().Maintenance.Snapshots.Configs[0]
	if !c.Allowed || c.Total != 2 || len(c.List) != 2 || c.List[0].Number != 2 || c.List[0].Type != "post" {
		t.Fatalf("config = %+v", c)
	}
	e.Tick()
	e.Tick()
	if n := fb.Called(maint.SnapperDest); n != 1 {
		t.Fatalf("snapperd asked %d times, want 1 (only on entering)", n)
	}
	e.RefreshMaintenance()
	e.Tick()
	if n := fb.Called(maint.SnapperDest); n != 2 {
		t.Fatalf("after refresh = %d, want 2", n)
	}
	e.SetFocus("")
	if s := e.Tick(); s.Maintenance.Snapshots == nil || fb.Called(maint.SnapperDest) != 2 {
		t.Fatal("leaving the section must keep the last read without asking")
	}
	e.SetFocus("maintenance")
	e.Tick()
	if n := fb.Called(maint.SnapperDest); n != 3 {
		t.Fatalf("on entering again = %d, want 3", n)
	}
}

// En el stream, medir las cachés no traba la muestra.
func TestMaintenanceReadDoesNotBlockTick(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	release := make(chan struct{})
	var calls atomic.Int32
	size := func(string) maint.DirSize { calls.Add(1); <-release; return maint.DirSize{SizeB: 1} }
	e := New(env, Options{Interval: 2 * time.Second, Focus: "maintenance", Stream: true, Statfs: fakeStatfs, DirSize: size})
	defer e.Close()
	done := make(chan *model.Snapshot)
	go func() {
		var s *model.Snapshot
		for range 3 {
			s = e.Tick()
		}
		done <- s
	}()
	select {
	case s := <-done:
		if s.Maintenance.Caches != nil {
			t.Fatal("caches before the walk answered")
		}
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("Tick waited for the caches")
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("walks = %d while the first one is running, want 1", n)
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for len(e.Tick().Maintenance.Caches) != 4 {
		if time.Now().After(deadline) {
			t.Fatal("the caches never arrived")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func updatesFixture(t *testing.T) source.Env {
	t.Helper()
	env := source.FixtureCopy(t, "threadripper")
	cmd := filepath.Join(filepath.Dir(env.FS.Root), "cmd")
	for name, body := range map[string]string{
		source.Key("checkupdates", "--nocolor"):                                               "linux 7.2.5-3 -> 7.2.6-1\n",
		source.Key("pacman", "-Sddp", "--print-format", "%r %n", "--dbpath", testDB, "linux"): "core linux\n",
	} {
		if err := os.WriteFile(filepath.Join(cmd, name+".txt"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return env
}

func TestCheckUpdatesInBackground(t *testing.T) {
	env := updatesFixture(t)
	e := maintEngine(t, env, "maintenance", true)
	defer e.Close()
	if u := e.Tick().Maintenance.Updates; u != nil {
		t.Fatalf("updates without u = %+v", u)
	}
	e.CheckUpdates()
	deadline := time.Now().Add(2 * time.Second)
	var u *model.Updates
	for {
		if u = e.Tick().Maintenance.Updates; u != nil && !u.Running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the check never arrived")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if u.Error != "" || len(u.Repo) != 1 || u.Repo[0] != (model.PkgUpdate{Name: "linux", Old: "7.2.5-3", New: "7.2.6-1", Repo: "core"}) ||
		u.Stale || !u.Checked.Equal(source.FixtureNow) || e.UpdatesRunning() {
		t.Fatalf("updates = %+v", u)
	}
	if runs(env).Count("yay") != 0 || runs(env).Count("paru") != 0 {
		t.Fatal("the AUR must never be asked")
	}
	// Sobrevive a un reinicio: otro engine lo lee de updates.json, también
	// con la sección cerrada.
	if _, err := os.Stat(filepath.Join(env.Cache, maint.UpdatesFile)); err != nil {
		t.Fatal(err)
	}
	if u := maintEngine(t, env, "", false).Tick().Maintenance.Updates; u == nil || len(u.Repo) != 1 || u.Running {
		t.Fatalf("reloaded = %+v", u)
	}
}

// blockingCheck frena checkupdates hasta que se cierra release.
type blockingCheck struct {
	*source.FakeRunner
	release chan struct{}
	n       atomic.Int32
}

func (b *blockingCheck) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if name == "checkupdates" {
		b.n.Add(1)
		<-b.release
		return nil, &source.ExitCode{Code: 2}
	}
	return b.FakeRunner.Run(ctx, name, args...)
}

func TestCheckUpdatesOneAtATime(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	br := &blockingCheck{FakeRunner: runs(env), release: make(chan struct{})}
	env.Run = br
	e := maintEngine(t, env, "", true)
	defer e.Close()
	e.CheckUpdates()
	e.CheckUpdates() // mientras corre: no lanza otra
	if u := e.Tick().Maintenance.Updates; u == nil || !u.Running || !e.UpdatesRunning() {
		t.Fatalf("while running = %+v", u)
	}
	close(br.release)
	deadline := time.Now().Add(2 * time.Second)
	for e.UpdatesRunning() {
		if time.Now().After(deadline) {
			t.Fatal("the check never finished")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if u := e.Tick().Maintenance.Updates; u == nil || u.Running || u.Error != "" || len(u.Repo) != 0 || br.n.Load() != 1 {
		t.Fatalf("after = %+v, checkupdates ran %d times", u, br.n.Load())
	}
}

// Otro proceso (lazylazy) escribe updates.json: con la sección abierta se
// ve en la muestra siguiente; una comprobación anterior al último cambio de
// paquetes queda vieja.
func TestUpdatesFileReloadAndStale(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	p := filepath.Join(env.Cache, maint.UpdatesFile)
	old := &model.Updates{Checked: time.Date(2026, 9, 22, 20, 0, 0, 0, time.UTC), Repo: []model.PkgUpdate{{Name: "a"}}}
	if err := SaveUpdates(env.Cache, old); err != nil {
		t.Fatal(err)
	}
	e := maintEngine(t, env, "maintenance", false)
	if u := e.Tick().Maintenance.Updates; u == nil || !u.Stale || len(u.Repo) != 1 {
		t.Fatalf("stale = %+v", u)
	}
	fresh := &model.Updates{Checked: source.FixtureNow, Repo: []model.PkgUpdate{{Name: "a"}, {Name: "b"}, {Name: "c"}}}
	SaveUpdates(env.Cache, fresh)
	later := time.Now().Add(time.Minute)
	os.Chtimes(p, later, later)
	if u := e.Tick().Maintenance.Updates; u == nil || u.Stale || len(u.Repo) != 3 {
		t.Fatalf("reloaded = %+v", u)
	}
	var saved map[string]any
	data, _ := os.ReadFile(p)
	if json.Unmarshal(data, &saved) != nil || saved["running"] != nil || saved["stale"] != nil || saved["checked"] == nil {
		t.Fatalf("updates.json = %s", data)
	}
}
