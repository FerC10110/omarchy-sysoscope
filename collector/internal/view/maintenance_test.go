package view

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/engine"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/maint"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// maintSnap es el segundo tick de un engine con Mantenimiento abierta sobre
// un Env de fixture, con los tamaños de su dirsizes.json.
func maintSnap(t *testing.T, env source.Env) *model.Snapshot {
	t.Helper()
	sizes, err := maint.LoadDirSizes(filepath.Join(filepath.Dir(env.FS.Root), maint.SizesFile))
	if err != nil {
		t.Fatal(err)
	}
	e := engine.New(env, engine.Options{Interval: 2 * time.Second, Focus: "maintenance", Statfs: fakeStatfs, DirSize: sizes})
	e.Tick()
	return e.Tick()
}

func findTable(bs []Block, title string) Block {
	var out Block
	walk(bs, func(b Block) {
		if b.Type == "table" && b.Title == title {
			out = b
		}
	})
	return out
}

func textsOf(bs []Block) []string {
	var out []string
	walk(bs, func(b Block) {
		if b.Type == "text" {
			out = append(out, b.Text)
		}
	})
	return out
}

func putFile(t *testing.T, root, rel, s string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMaintenanceViewThreadripper(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	s := maintSnap(t, env)
	v, err := Build("maintenance", s, Options{Lang: "en"})
	if err != nil {
		t.Fatal(err)
	}
	h := v.Blocks[0]
	if h.Type != "hero" || len(h.Blocks) != 4 || h.Blocks[0].Label != "Restart needed" || h.Blocks[0].Text != "No" || h.Blocks[0].Level != "" ||
		h.Blocks[1].Label != "Last update" || h.Blocks[1].Text != "12 h ago" ||
		h.Blocks[2].Label != "Caches" || h.Blocks[2].Text != "7.8" || h.Blocks[2].Unit != "GiB" {
		t.Fatalf("hero = %+v", h)
	}
	walk(v.Blocks, func(b Block) {
		if b.Level == "warn" {
			t.Errorf("a warning on a machine that needs nothing: %+v", b)
		}
	})
	for k, want := range map[string]string{"Running kernel": "7.2.5-3-omarchy", "Installed kernels": "7.2.3-arch1-3, 7.2.5-3-omarchy",
		"NVIDIA driver loaded": "610.57.04", "NVIDIA driver installed": "610.57.04", "Hyprland": "Up to date"} {
		if got := kvValue(v.Blocks, "Restart needed", k); got != want {
			t.Errorf("Restart %s = %q, want %q", k, got, want)
		}
	}
	full := time.Date(2026, 9, 22, 23, 41, 32, 0, time.UTC).Local().Format("2006-01-02 15:04") + " (12 h ago)"
	change := time.Date(2026, 9, 22, 23, 42, 25, 0, time.UTC).Local().Format("2006-01-02 15:04") + " (12 h ago)"
	if kvValue(v.Blocks, "Updates", "Last full update") != full || kvValue(v.Blocks, "Updates", "Packages updated") != "2" ||
		kvValue(v.Blocks, "Updates", "Last package change") != change {
		t.Errorf("updates kv = %+v", v.Blocks)
	}
	c := tableRows(findTable(v.Blocks, "Disk space used by caches"))
	if len(c) != 4 || !reflect.DeepEqual(c[0].Cells, []string{"Package cache (pacman)", "6.3 GiB (partial)", "sudo paccache -rk2 · sudo paccache -ruk0"}) ||
		c[1].Cells[1] != "489.0 MiB" || c[2].Cells[0] != "System journal" || c[2].Cells[1] != "348.1 MiB" ||
		c[3].Cells[2] != "Removed automatically by systemd" {
		t.Errorf("caches = %+v", c)
	}
	all := strings.Join(textsOf(v.Blocks), "\n")
	for _, want := range []string{
		"Measured just now",
		"Updates are checked on request (press u). This downloads the package lists from your pacman mirrors; nothing about your system is sent.",
		"AUR packages are not checked (that would send their names to aur.archlinux.org)",
		"Snapshots are not visible to your user. To see them here, run once: sudo snapper -c root set-config ALLOW_USERS=user",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing text %q in:\n%s", want, all)
		}
	}
	if g := findGroup(v.Blocks, "Snapshots"); len(g.Blocks) != 1 {
		t.Errorf("snapshots group = %+v", g)
	}
	if len(v.Actions) != 1 || v.Actions[0].Key != "u" || v.Actions[0].ID != "updates.check" || v.Actions[0].Target != "section" ||
		v.Actions[0].Mode != "background" || v.Actions[0].Label != "Check for updates" {
		t.Errorf("actions = %+v", v.Actions)
	}
	noEmptyValues(t, v)
	if bus := env.Bus.(*source.FakeBus); bus.Called(maint.SnapperDest) != 0 {
		t.Error("snapperd asked without permission")
	}
}

func TestMaintenanceViewSpanish(t *testing.T) {
	s := maintSnap(t, source.Fixture(t, "threadripper"))
	v, err := Build("maintenance", s, Options{Lang: "es"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Title != "Mantenimiento" || v.Blocks[0].Blocks[0].Label != "Reinicio pendiente" || v.Blocks[0].Blocks[1].Text != "hace 12 h" ||
		kvValue(v.Blocks, "Reinicio pendiente", "Kernel en uso") != "7.2.5-3-omarchy" || v.Actions[0].Label != "Buscar actualizaciones" ||
		len(tableRows(findTable(v.Blocks, "Espacio en disco de las cachés"))) != 4 {
		t.Errorf("es view = %+v", v)
	}
	if !strings.Contains(strings.Join(textsOf(v.Blocks), "\n"), "Los snapshots no son visibles para este usuario") {
		t.Errorf("es texts = %v", textsOf(v.Blocks))
	}
}

// Una máquina que pide reiniciar: kernel quitado, marca de Omarchy,
// Hyprland reemplazado y driver NVIDIA actualizado.
func TestMaintenanceNeedsRestart(t *testing.T) {
	env := source.FixtureCopy(t, "threadripper")
	root := env.FS.Root
	os.Remove(filepath.Join(root, "usr/lib/modules/7.2.5-3-omarchy/vmlinuz"))
	putFile(t, root, "home/user/.local/state/omarchy/reboot-required", "")
	putFile(t, root, "proc/613755/exe.link", "/usr/bin/Hyprland (deleted)\n")
	os.Rename(filepath.Join(root, "var/lib/pacman/local/nvidia-utils-610.57.04-1"), filepath.Join(root, "var/lib/pacman/local/nvidia-utils-610.60.01-1"))
	s := maintSnap(t, env)
	w := Warnings(s, Options{Lang: "en"})
	var got []string
	for _, b := range w {
		if b.Link == "maintenance" {
			if b.Level != "warn" {
				t.Errorf("level = %q", b.Level)
			}
			got = append(got, b.Text)
		}
	}
	want := []string{
		"Restart needed: the running kernel (7.2.5-3-omarchy) is no longer installed",
		"Restart needed after the last update",
		"Hyprland was updated: log out and back in to use it",
		"NVIDIA driver updated (loaded 610.57.04, installed 610.60.01): restart to use it",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("warnings = %q", got)
	}
	secs := Sections(s, Options{Lang: "en"})
	for _, sec := range secs {
		if sec.ID == "maintenance" && sec.Badge != 4 {
			t.Errorf("maintenance badge = %d", sec.Badge)
		}
	}
	if secs[0].Badge != len(w) {
		t.Errorf("summary badge = %d, warnings %d", secs[0].Badge, len(w))
	}
	v, _ := Build("maintenance", s, Options{Lang: "en"})
	if h := v.Blocks[0].Blocks[0]; h.Text != "Yes" || h.Level != "warn" {
		t.Errorf("restart stat = %+v", h)
	}
	for i, t2 := range want {
		if b := v.Blocks[1+i]; b.Type != "text" || b.Text != t2 || b.Level != "warn" {
			t.Errorf("block %d = %+v", 1+i, b)
		}
	}
	if kvValue(v.Blocks, "Restart needed", "Hyprland") != "Updated: log out and back in to use it" ||
		kvValue(v.Blocks, "Restart needed", "Installed kernels") != "7.2.3-arch1-3" {
		t.Errorf("restart kv = %+v", v.Blocks)
	}
	es := Warnings(s, Options{Lang: "es"})
	found := false
	for _, b := range es {
		found = found || b.Text == "Hace falta reiniciar tras la última actualización"
	}
	if !found {
		t.Errorf("es warnings = %+v", es)
	}
}

// Review Focus 5: días sin actualizar, comprobaciones frescas o viejas, y
// fechas en el futuro.
func TestMaintenanceWarningDays(t *testing.T) {
	s := maintSnap(t, source.Fixture(t, "threadripper"))
	up := s.Maintenance.Upgrade
	check := func(name string, want ...string) {
		t.Helper()
		if got := updateWarnings(s, "en"); !reflect.DeepEqual(got, want) && !(len(got) == 0 && len(want) == 0) {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	up.LastFull = s.Time.Add(-31 * 24 * time.Hour)
	check("31 days", "No full system update in 31 days")
	s.Maintenance.Updates = &model.Updates{Checked: s.Time, Repo: []model.PkgUpdate{}}
	check("a fresh check with nothing pending")
	s.Maintenance.Updates.Repo = []model.PkgUpdate{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	check("pending", "No full system update in 31 days", "3 updates available")
	s.Maintenance.Updates.Stale = true
	check("stale", "No full system update in 31 days")
	s.Maintenance.Updates = &model.Updates{Checked: s.Time, Error: "timed out", Repo: []model.PkgUpdate{}}
	check("error", "No full system update in 31 days")
	s.Maintenance.Updates = &model.Updates{Checked: s.Time, Repo: []model.PkgUpdate{{Name: "a"}}}
	up.LastFull = s.Time.Add(-time.Hour)
	check("one", "1 update available")
	s.Maintenance.Updates = nil
	up.LastFull = s.Time.Add(-30*24*time.Hour - time.Hour)
	check("30 days is not more than 30")
	up.LastFull = s.Time.Add(72 * time.Hour) // reloj corrido
	check("future")
	if h := maintenanceHero(s, Options{Lang: "en"}); h.Blocks[1].Text != "just now" {
		t.Errorf("future hero = %+v", h.Blocks[1])
	}
	up.LastFull = time.Time{}
	check("never")
	v, _ := Build("maintenance", s, Options{Lang: "en"})
	if kvValue(v.Blocks, "Updates", "Last full update") != "No full update recorded yet" || v.Blocks[0].Blocks[1].Text != "—" {
		t.Errorf("never = %+v", v.Blocks)
	}
}

func TestAgo(t *testing.T) {
	for d, want := range map[time.Duration]string{
		-3 * time.Hour: "just now", 0: "just now", 59 * time.Second: "just now",
		time.Minute: "1 min ago", 59 * time.Minute: "59 min ago", time.Hour: "1 h ago",
		47*time.Hour + 59*time.Minute: "47 h ago", 48 * time.Hour: "2 days ago", 40 * 24 * time.Hour: "40 days ago",
	} {
		if got := Ago(d, "en"); got != want {
			t.Errorf("Ago(%v) = %q, want %q", d, got, want)
		}
	}
	if Ago(3*time.Hour, "es") != "hace 3 h" || Ago(0, "es") != "recién" || Ago(72*time.Hour, "es") != "hace 3 días" {
		t.Errorf("es = %q %q %q", Ago(3*time.Hour, "es"), Ago(0, "es"), Ago(72*time.Hour, "es"))
	}
}

func TestMaintenanceUpdatesView(t *testing.T) {
	s := maintSnap(t, source.Fixture(t, "threadripper"))
	build := func() *View {
		v, err := Build("maintenance", s, Options{Lang: "en"})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	var repo []model.PkgUpdate
	for i := range 102 {
		p := model.PkgUpdate{Name: "p" + strings.Repeat("x", i%3) + string(rune('a'+i%26)), Old: "1-1", New: "2-1"}
		if i == 0 {
			p = model.PkgUpdate{Name: "linux", Old: "7.2.5-3", New: "7.2.6-1", Repo: "core"}
		}
		repo = append(repo, p)
	}
	s.Maintenance.Updates = &model.Updates{Checked: s.Time.Add(-5 * time.Minute), Repo: repo}
	v := build()
	rs := tableRows(findTable(v.Blocks, "Available updates"))
	all := strings.Join(textsOf(v.Blocks), "\n")
	if len(rs) != 100 || !reflect.DeepEqual(rs[0].Cells, []string{"linux", "7.2.5-3", "7.2.6-1", "core"}) || rs[1].Cells[3] != "—" ||
		!strings.Contains(all, "and 2 more") || !strings.Contains(all, "102 updates available") ||
		!strings.Contains(all, "AUR packages are not checked (that would send their names to aur.archlinux.org)") ||
		strings.Contains(all, "press u)") {
		t.Errorf("pending: %d rows, texts:\n%s", len(rs), all)
	}
	if findTable(v.Blocks, "Available updates").Selectable {
		t.Error("the updates table has no row actions")
	}
	s.Maintenance.Updates = &model.Updates{Running: true, Repo: []model.PkgUpdate{}}
	if all := strings.Join(textsOf(build().Blocks), "\n"); !strings.Contains(all, "Checking for updates…") || strings.Contains(all, "press u)") {
		t.Errorf("running:\n%s", all)
	}
	s.Maintenance.Updates = &model.Updates{Checked: s.Time, Error: "requires pacman-contrib", Repo: []model.PkgUpdate{}}
	if all := strings.Join(textsOf(build().Blocks), "\n"); !strings.Contains(all, "Could not check for updates: requires pacman-contrib") {
		t.Errorf("error:\n%s", all)
	}
	s.Maintenance.Updates = &model.Updates{Checked: s.Time, Stale: true, Repo: repo}
	if v := build(); len(tableRows(findTable(v.Blocks, "Available updates"))) != 0 ||
		!strings.Contains(strings.Join(textsOf(v.Blocks), "\n"), "Checked before the last package change; press u to check again") {
		t.Errorf("stale = %+v", v.Blocks)
	}
	// Review fix 5: a new check already running makes the "press u again" nag redundant.
	s.Maintenance.Updates = &model.Updates{Checked: s.Time, Stale: true, Running: true, Repo: repo}
	if all := strings.Join(textsOf(build().Blocks), "\n"); !strings.Contains(all, "Checking for updates…") ||
		strings.Contains(all, "press u to check again") {
		t.Errorf("stale while running:\n%s", all)
	}
	s.Maintenance.Updates = &model.Updates{Checked: s.Time.Add(-5 * time.Minute), Repo: []model.PkgUpdate{}}
	if all := strings.Join(textsOf(build().Blocks), "\n"); !strings.Contains(all, "No updates available (checked 5 min ago)") {
		t.Errorf("none:\n%s", all)
	}
}

func TestMaintenanceSnapshotsView(t *testing.T) {
	s := maintSnap(t, source.Fixture(t, "threadripper"))
	date := time.Date(2026, 9, 22, 23, 41, 0, 0, time.UTC)
	s.Maintenance.Snapshots = &model.Snapshots{User: "user", Configs: []model.SnapConfig{
		{Name: "root", Allowed: true, Total: 12, List: []model.Snap{
			{Number: 12, Date: date, Type: "post", Description: "omarchy 4.0.4", Cleanup: "number"}, {Number: 11, Type: "pre"}}},
		{Name: "home", AllowUsers: "ana"},
		{Name: "var", Allowed: true}, // permitido pero ListSnapshots falló: nada
		{Name: "boot", Allowed: true, List: []model.Snap{}},
		{Name: "efi", Allowed: true, List: []model.Snap{}},
	}}
	v, _ := Build("maintenance", s, Options{Lang: "en"})
	g := findGroup(v.Blocks, "Snapshots")
	tb := findTable(g.Blocks, "Snapshots of root (12)")
	rs := tableRows(tb)
	if len(rs) != 2 || !reflect.DeepEqual(rs[0].Cells, []string{"12", date.Local().Format("2006-01-02 15:04"), "After the change", "omarchy 4.0.4", "number"}) ||
		!reflect.DeepEqual(rs[1].Cells, []string{"11", "—", "Before the change", "—", "—"}) || tb.Selectable {
		t.Fatalf("snapshots table = %+v", tb)
	}
	if all := strings.Join(textsOf(g.Blocks), "\n"); !strings.Contains(all, `run once: sudo snapper -c home set-config ALLOW_USERS="ana user"`) {
		t.Errorf("home = %q", all)
	}
	// Review fix 2: the empty-config message names its own config, so two
	// empty configs don't read as the same message.
	if all := strings.Join(textsOf(g.Blocks), "\n"); !strings.Contains(all, "No snapshots of boot yet") || !strings.Contains(all, "No snapshots of efi yet") {
		t.Errorf("empty configs = %q", all)
	}
	es, _ := Build("maintenance", s, Options{Lang: "es"})
	if rs := tableRows(findTable(es.Blocks, "Snapshots de root (12)")); len(rs) != 2 || rs[0].Cells[2] != "Después del cambio" {
		t.Errorf("es snapshots = %+v", rs)
	}
	if esg := findGroup(es.Blocks, "Snapshots"); !strings.Contains(strings.Join(textsOf(esg.Blocks), "\n"), "Todavía no hay snapshots de boot") {
		t.Errorf("es empty configs = %q", textsOf(esg.Blocks))
	}
	s.Maintenance.Snapshots = nil
	if v, _ := Build("maintenance", s, Options{Lang: "en"}); findGroup(v.Blocks, "Snapshots").Type != "" {
		t.Error("no snapper: no snapshots group")
	}
}
