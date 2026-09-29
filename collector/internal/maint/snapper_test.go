package maint

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

const snapperService = "root/usr/share/dbus-1/system-services/org.opensuse.Snapper.service"

func snapperEnv(t *testing.T, conf string, configs map[string]string) (source.Env, string) {
	t.Helper()
	env, dir := tempEnv(t)
	put(t, dir, snapperService, "[D-BUS Service]\nName=org.opensuse.Snapper\n")
	if conf != "" {
		put(t, dir, "root/etc/conf.d/snapper", conf)
	}
	for name, c := range configs {
		put(t, dir, "root/etc/snapper/configs/"+name, c)
	}
	put(t, dir, "root/etc/group", "root:x:0:\nwheel:x:998:ana,user\nsnap:x:1001:\nusers:x:1000:\nbroken\n")
	return env, dir
}

// withSnapshots le da al FakeBus n snapshots del config root, más el 0
// ("current"), que no se muestra.
func withSnapshots(env source.Env, n int) (source.Env, *source.FakeBus) {
	items := []any{[]any{0, 0, 0, -1, 0, "current", "", map[string]any{}}}
	for i := 1; i <= n; i++ {
		items = append(items, []any{i, i % 3, 0, 1758580000 + i*60, 0, fmt.Sprintf("omarchy 4.0.%d", i), "number", map[string]any{}})
	}
	raw, _ := json.Marshal([]any{items})
	bus := &source.FakeBus{System: map[string]json.RawMessage{
		"call org.opensuse.Snapper /org/opensuse/Snapper org.opensuse.Snapper.ListSnapshots root": raw}}
	env.Bus = bus
	return env, bus
}

const closed = "SUBVOLUME=\"/\"\nFSTYPE=\"btrfs\"\nALLOW_USERS=\"\"\nALLOW_GROUPS=\"\"\n"

// Review Focus 4: sin permiso no se habla con snapperd.
func TestSnapperNotAllowedNoBus(t *testing.T) {
	env, dir := snapperEnv(t, "SNAPPER_CONFIGS=\"root\"\n", map[string]string{"root": closed})
	env, bus := withSnapshots(env, 3)
	r := &collect.Report{Lang: "en"}
	got := Snapper(env, true, r)
	want := &model.Snapshots{User: "user", Configs: []model.SnapConfig{{Name: "root"}}}
	if !reflect.DeepEqual(got, want) || len(r.List) != 0 {
		t.Fatalf("snapshots = %+v, unavailable %+v", got, r.List)
	}
	if Snapper(env, false, r) != nil {
		t.Error("/ is not btrfs: no snapshots block")
	}
	os.Remove(filepath.Join(dir, snapperService))
	if Snapper(env, true, r) != nil {
		t.Error("snapper not installed: no snapshots block")
	}
	if n := bus.Called(SnapperDest); n != 0 {
		t.Fatalf("snapperd asked %d times without permission", n)
	}
}

func TestSnapperAllowedByUserOrGroup(t *testing.T) {
	cases := []struct {
		name, config string
		user         source.User
		allowed      bool
	}{
		{"user listed", "ALLOW_USERS=\"ana user\"\n", source.FixtureUser, true},
		{"single quotes", "# ALLOW_USERS=\"nobody\"\nALLOW_USERS='user'\n", source.FixtureUser, true},
		{"group by member", "ALLOW_GROUPS=\"wheel\"\n", source.FixtureUser, true},
		{"group by gid", "ALLOW_GROUPS=\"snap\"\n", source.User{UID: 1000, Name: "user", GIDs: []uint32{1000, 1001}}, true},
		{"root", closed, source.User{UID: 0, Name: "root", GIDs: []uint32{0}}, true},
		{"other user only", "ALLOW_USERS=\"username\"\nALLOW_GROUPS=\"nobody\"\n", source.FixtureUser, false},
	}
	for _, c := range cases {
		env, _ := snapperEnv(t, "SNAPPER_CONFIGS=\"root\"\n", map[string]string{"root": c.config})
		env.User = c.user
		env, bus := withSnapshots(env, 12)
		r := &collect.Report{Lang: "en"}
		got := Snapper(env, true, r)
		if got == nil || len(got.Configs) != 1 {
			t.Fatalf("%s: snapshots = %+v", c.name, got)
		}
		cfg := got.Configs[0]
		if cfg.Allowed != c.allowed || len(r.List) != 0 {
			t.Errorf("%s: allowed = %v (%+v)", c.name, cfg.Allowed, r.List)
			continue
		}
		if !c.allowed {
			if cfg.List != nil || bus.Called(SnapperDest) != 0 {
				t.Errorf("%s: read without permission", c.name)
			}
			continue
		}
		first := model.Snap{Number: 12, Date: time.Unix(1758580000+12*60, 0).UTC(), Type: "single",
			Description: "omarchy 4.0.12", Cleanup: "number"}
		if cfg.Total != 12 || len(cfg.List) != 10 || cfg.List[0] != first || cfg.List[9].Number != 3 ||
			cfg.List[1].Type != "post" || cfg.List[2].Type != "pre" || bus.Called(SnapperDest) != 1 {
			t.Errorf("%s: list = %d of %d, first %+v, calls %d", c.name, len(cfg.List), cfg.Total, cfg.List[0], bus.Called(SnapperDest))
		}
	}
}

func TestSnapperConfigList(t *testing.T) {
	// Un config listado que no existe y un nombre con barra: se saltean.
	env, _ := snapperEnv(t, "SNAPPER_CONFIGS=\"root home ../etc\"\n", map[string]string{"root": closed})
	got := Snapper(env, true, &collect.Report{Lang: "en"})
	if got == nil || len(got.Configs) != 1 || got.Configs[0].Name != "root" {
		t.Fatalf("configs = %+v", got)
	}
	// Sin /etc/conf.d/snapper: los archivos de configs/, en orden.
	env, _ = snapperEnv(t, "", map[string]string{"root": closed, "home": closed})
	got = Snapper(env, true, &collect.Report{Lang: "en"})
	if got == nil || len(got.Configs) != 2 || got.Configs[0].Name != "home" || got.Configs[1].Name != "root" {
		t.Fatalf("configs from the directory = %+v", got)
	}
	// Ningún config legible: sin bloque.
	env, _ = snapperEnv(t, "SNAPPER_CONFIGS=\"gone\"\n", map[string]string{"root": closed})
	if got := Snapper(env, true, &collect.Report{Lang: "en"}); got != nil {
		t.Fatalf("no readable config = %+v", got)
	}
	// El comando para habilitar necesita los usuarios que ya estaban.
	env, _ = snapperEnv(t, "", map[string]string{"root": "ALLOW_USERS=\"ana\"\n"})
	got = Snapper(env, true, &collect.Report{Lang: "en"})
	if got == nil || len(got.Configs) != 1 || got.Configs[0].Allowed || got.Configs[0].AllowUsers != "ana" {
		t.Fatalf("allow users = %+v", got)
	}
}

// Fix review ronda 1, punto 2: un error de lectura que no es "no existe"
// (permission denied en un config 0600) no desaparece en silencio: queda en
// unavailable y el resto de los configs legibles se conserva.
func TestSnapperConfigUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every file: the permission case cannot be built")
	}
	env, dir := snapperEnv(t, "SNAPPER_CONFIGS=\"home root\"\n", map[string]string{"root": closed, "home": closed})
	locked := filepath.Join(dir, "root/etc/snapper/configs/root")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o644) })
	r := &collect.Report{Lang: "en"}
	got := Snapper(env, true, r)
	if got == nil || len(got.Configs) != 1 || got.Configs[0].Name != "home" {
		t.Fatalf("configs = %+v", got)
	}
	if len(r.List) != 1 || r.List[0].What != "maintenance.snapshots" || r.List[0].Reason != "permission" {
		t.Fatalf("unavailable = %+v", r.List)
	}
}

func TestSnapperBusError(t *testing.T) {
	env, _ := snapperEnv(t, "", map[string]string{"root": "ALLOW_USERS=\"user\"\n"})
	r := &collect.Report{Lang: "en"}
	got := Snapper(env, true, r) // el FakeBus vacío no responde ListSnapshots
	if got == nil || len(got.Configs) != 1 {
		t.Fatalf("snapshots = %+v", got)
	}
	if c := got.Configs[0]; !c.Allowed || c.List != nil || c.Total != 0 {
		t.Fatalf("config = %+v", c)
	}
	if len(r.List) != 1 || r.List[0].What != "maintenance.snapshots" || r.List[0].Reason != "error" {
		t.Fatalf("unavailable = %+v", r.List)
	}
}

// Fix review ronda 1, punto 1: una respuesta de snapperd con forma
// inesperada (no lo que ListSnapshots debería devolver) no se confunde con
// "sin snapshots": queda en unavailable, no en una lista vacía.
func TestSnapperUnexpectedShape(t *testing.T) {
	cases := []struct {
		name string
		body []any
	}{
		{"body[0] is not a list", []any{"nope"}},
		{"an item is not a list", []any{[]any{"nope"}}},
		{"an item is too short", []any{[]any{[]any{1, 0, 0}}}},
		{"an item's number is not numeric", []any{[]any{
			[]any{"x", 0, 0, 0, 0, "d", "c", map[string]any{}}}}},
	}
	for _, c := range cases {
		env, _ := snapperEnv(t, "", map[string]string{"root": "ALLOW_USERS=\"user\"\n"})
		raw, err := json.Marshal(c.body)
		if err != nil {
			t.Fatal(err)
		}
		env.Bus = &source.FakeBus{System: map[string]json.RawMessage{
			"call org.opensuse.Snapper /org/opensuse/Snapper org.opensuse.Snapper.ListSnapshots root": raw}}
		r := &collect.Report{Lang: "en"}
		got := Snapper(env, true, r)
		if got == nil || len(got.Configs) != 1 {
			t.Fatalf("%s: snapshots = %+v", c.name, got)
		}
		if cfg := got.Configs[0]; !cfg.Allowed || cfg.List != nil || cfg.Total != 0 {
			t.Errorf("%s: config = %+v", c.name, cfg)
		}
		if len(r.List) != 1 || r.List[0].What != "maintenance.snapshots" || r.List[0].Reason != "error" {
			t.Errorf("%s: unavailable = %+v", c.name, r.List)
		}
	}
}

func TestGroupNames(t *testing.T) {
	env, _ := snapperEnv(t, "", nil)
	if got := GroupNames(env, source.User{UID: 1000, Name: "user", GIDs: []uint32{1000}}); !reflect.DeepEqual(got, []string{"wheel", "users"}) {
		t.Errorf("groups = %v", got)
	}
	if got := GroupNames(env, source.User{UID: 5, GIDs: []uint32{5}}); len(got) != 0 {
		t.Errorf("nameless user = %v", got)
	}
}

func TestRootBtrfs(t *testing.T) {
	fs := func(fstype string, mounts ...string) *model.Storage {
		return &model.Storage{Filesystems: []model.Filesystem{{FSType: fstype, Mounts: mounts}}}
	}
	if !RootBtrfs(fs("btrfs", "/home", "/")) || RootBtrfs(fs("ext4", "/")) || RootBtrfs(fs("btrfs", "/home")) || RootBtrfs(nil) {
		t.Error("RootBtrfs")
	}
}
