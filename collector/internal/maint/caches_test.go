package maint

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

func blocks(t *testing.T, p string) uint64 {
	t.Helper()
	var st unix.Stat_t
	if err := unix.Lstat(p, &st); err != nil {
		t.Fatal(err)
	}
	return uint64(st.Blocks) * 512
}

// Review Focus 3: un enlace no se sigue; un subdirectorio sin permiso se
// saltea y deja la medida "parcial".
func TestWalkSymlinksAndPartial(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "big")
	os.WriteFile(outside, make([]byte, 1<<20), 0o644)
	os.WriteFile(filepath.Join(root, "a"), make([]byte, 8192), 0o644)
	os.MkdirAll(filepath.Join(root, "sub"), 0o755)
	os.WriteFile(filepath.Join(root, "sub", "b"), make([]byte, 100), 0o644)
	os.Symlink(outside, filepath.Join(root, "link"))
	os.Symlink(filepath.Dir(outside), filepath.Join(root, "dirlink"))
	var want uint64
	for _, p := range []string{"", "a", "sub", "sub/b", "link", "dirlink"} {
		want += blocks(t, filepath.Join(root, p))
	}
	got := Walk(root, WalkMax)
	if got.SizeB != want || got.Partial || got.SizeB >= 1<<20 {
		t.Fatalf("walk = %+v, want %d bytes and not partial (1 MiB means the link was followed)", got, want)
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads every directory: the partial case cannot be built")
	}
	locked := filepath.Join(root, "download-x")
	os.MkdirAll(locked, 0o755)
	os.WriteFile(filepath.Join(locked, "part"), make([]byte, 4096), 0o644)
	os.Chmod(locked, 0)
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	got = Walk(root, WalkMax)
	if !got.Partial || got.SizeB != want+blocks(t, locked) {
		t.Fatalf("locked = %+v, want partial and %d bytes", got, want+blocks(t, locked))
	}
}

// Una raíz de caché que es un symlink (p. ej. ~/.cache/yay a otro disco, o
// un bind mount armado con un enlace) no debe medir 0 B: se resuelve la
// raíz misma (no lo que hay adentro, que sigue sin seguir enlaces).
func TestWalkResolvesSymlinkRoot(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "a"), make([]byte, 8192), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(target, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "sub", "b"), make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "yay")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	want := Walk(target, WalkMax)
	if got := Walk(link, WalkMax); got != want || got.SizeB == 0 {
		t.Fatalf("symlink root = %+v, want lo mismo que el destino resuelto %+v", got, want)
	}
}

func TestWalkCap(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"1", "2", "3", "4", "5"} {
		os.WriteFile(filepath.Join(root, n), []byte(n), 0o644)
	}
	if got := Walk(root, 2); !got.Partial {
		t.Errorf("cap 2 = %+v, want partial", got)
	}
	if got := Walk(root, 100); got.Partial {
		t.Errorf("cap 100 = %+v", got)
	}
	if got := Walk(filepath.Join(root, "missing"), 100); !got.Partial || got.SizeB != 0 {
		t.Errorf("missing root = %+v", got)
	}
}

func TestPacmanCacheDir(t *testing.T) {
	for conf, want := range map[string]string{
		"":                             "/var/cache/pacman/pkg",
		"[options]\n#CacheDir = /x/\n": "/var/cache/pacman/pkg",
		"[options]\nCacheDir    = /data/pkg/  /other/\n": "/data/pkg",
		"[options]\nCacheDir = relative\n":               "/var/cache/pacman/pkg",
		"[core]\nCacheDir = /nope\n":                     "/var/cache/pacman/pkg",
		"[options]\nCacheDir = /a # comment\n":           "/a",
	} {
		env, dir := tempEnv(t)
		if conf != "" {
			put(t, dir, "root/etc/pacman.conf", conf)
		}
		if got := PacmanCacheDir(env); got != want {
			t.Errorf("%q = %q, want %q", conf, got, want)
		}
	}
}

func TestCaches(t *testing.T) {
	env, dir := tempEnv(t)
	put(t, dir, "root/var/cache/pacman/pkg/.keep", "")
	put(t, dir, "root/home/user/.cache/yay/.keep", "")
	put(t, dir, "root/var/lib/systemd/coredump/.keep", "")
	put(t, dir, "cmd/journalctl_--disk-usage.txt", "Archived and active journals take up 348.1M in the file system.\n")
	sizes := map[string]DirSize{
		"/var/cache/pacman/pkg":     {SizeB: 6764573491, Partial: true},
		"/home/user/.cache/yay":     {SizeB: 512753664},
		"/var/lib/systemd/coredump": {SizeB: 684720128},
	}
	var asked []string
	size := func(p string) DirSize { asked = append(asked, p); return sizes[p] }
	r := &collect.Report{Lang: "en"}
	got := Caches(env, size, r)
	want := []model.CacheDir{
		{ID: "pacman", Path: "/var/cache/pacman/pkg", SizeB: 6764573491, Partial: true},
		{ID: "yay", Path: "/home/user/.cache/yay", SizeB: 512753664},
		{ID: "journal", SizeB: 365009306},
		{ID: "coredump", Path: "/var/lib/systemd/coredump", SizeB: 684720128},
	}
	if !reflect.DeepEqual(got, want) || len(r.List) != 0 || len(asked) != 3 {
		t.Fatalf("caches = %+v (asked %v, unavailable %+v)", got, asked, r.List)
	}
	// Sin journalctl: la fila del journal no está y se explica.
	os.Remove(filepath.Join(dir, "cmd/journalctl_--disk-usage.txt"))
	r = &collect.Report{Lang: "en"}
	got = Caches(env, size, r)
	if len(got) != 3 || got[2].ID != "coredump" || len(r.List) != 1 || r.List[0].What != "maintenance.caches.journal" ||
		r.List[0].Reason != "missing_tool" || r.List[0].Message != "requires journalctl" {
		t.Fatalf("without journalctl = %+v %+v", got, r.List)
	}
	// Sin coredump: el journal va último.
	os.RemoveAll(filepath.Join(dir, "root/var/lib/systemd/coredump"))
	put(t, dir, "cmd/journalctl_--disk-usage.txt", "Archived and active journals take up 1.5G in the file system.\n")
	if got := Caches(env, size, &collect.Report{Lang: "en"}); len(got) != 3 || got[2].ID != "journal" || got[2].SizeB != 1610612736 {
		t.Fatalf("without coredump = %+v", got)
	}
}

func TestDiskUsage(t *testing.T) {
	for in, want := range map[string]uint64{
		"Archived and active journals take up 512B in the file system.":   512,
		"Journals take up 8.0K on disk.":                                  8192,
		"Archived and active journals take up 348.1M in the file system.": 365009306,
		"Archived and active journals take up 2G in the file system.":     2 << 30,
	} {
		if got, ok := diskUsage(in); !ok || got != want {
			t.Errorf("%q = %d, %v; want %d", in, got, ok, want)
		}
	}
	if _, ok := diskUsage("No journal files were found."); ok {
		t.Error("garbage parsed")
	}
}

func TestLoadDirSizes(t *testing.T) {
	p := filepath.Join(t.TempDir(), SizesFile)
	os.WriteFile(p, []byte(`{"/var/cache/pacman/pkg":{"size_b":10,"partial":true}}`), 0o644)
	f, err := LoadDirSizes(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := f("/var/cache/pacman/pkg"); got != (DirSize{SizeB: 10, Partial: true}) {
		t.Errorf("pacman = %+v", got)
	}
	if got := f("/elsewhere"); got != (DirSize{}) {
		t.Errorf("unknown = %+v", got)
	}
	if _, err := LoadDirSizes(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("missing file loaded")
	}
}
