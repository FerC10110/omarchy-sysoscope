package apps

import (
	"os"
	"strings"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

func TestParseStat(t *testing.T) {
	p, ok := parseStat("42 (Web (Content)) S 7 0 0 0 -1 0 0 0 0 0 30 12 0 0 20 0 1 0 999 0 10")
	if !ok || p.pid != 42 || p.comm != "Web (Content)" || p.ppid != 7 || p.cpu != 42 || p.start != 999 || p.rssB != 10*pageSize {
		t.Fatalf("parsed = %+v, %v", p, ok)
	}
	for _, bad := range []string{"", "42 comm S 1", "x (a) S 1 0 0 0 -1 0 0 0 0 0 1 1 0 0 20 0 1 0 1 0 1", "42 (a) S 1"} {
		if _, ok := parseStat(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestReadProcsFixture(t *testing.T) {
	env := source.Fixture(t, "apps")
	ps := readProcs(env.FS, uid(), map[int]meta{})
	if len(ps) != 16 {
		t.Fatalf("%d processes, want 16", len(ps))
	}
	by := map[int]proc{}
	for _, p := range ps {
		by[p.pid] = p
	}
	if p := by[201]; p.ppid != 200 || !strings.HasSuffix(p.cgroup, "/app-org.chromium.Chromium-200.scope") || p.cpu != 4000 {
		t.Fatalf("201 = %+v", p)
	}
	if by[700].comm != "Web Content" {
		t.Fatalf("700 comm = %q", by[700].comm)
	}
}

func TestReadProcsOtherUser(t *testing.T) {
	env := source.Fixture(t, "apps")
	cache := map[int]meta{}
	if ps := readProcs(env.FS, uid()+1, cache); len(ps) != 0 {
		t.Fatalf("%d processes of another user", len(ps))
	}
	if m, ok := cache[200]; !ok || m.own {
		t.Fatalf("cache[200] = %+v, %v", m, ok)
	}
}

func TestReadProcsSkipsOthersFromCache(t *testing.T) {
	env := tempApps(t)
	cache := map[int]meta{}
	readProcs(env.FS, uid()+1, cache) // todos ajenos
	// Otro arranque en stat obligaría a mirar de nuevo al dueño: si se releyera
	// un proceso ajeno, 200 aparecería ahora como del usuario.
	os.WriteFile(env.FS.Real("/proc/200/stat"), []byte("200 (chromium) S 1 0 0 0 -1 0 0 0 0 0 1 1 0 0 20 0 1 0 999 0 1\n"), 0o644)
	if ps := readProcs(env.FS, uid(), cache); len(ps) != 0 {
		t.Fatalf("foreign processes were read again: %d", len(ps))
	}
}

func TestReadProcsPrunesCache(t *testing.T) {
	env := tempApps(t)
	cache := map[int]meta{}
	readProcs(env.FS, uid(), cache)
	os.RemoveAll(env.FS.Real("/proc/700"))
	ps := readProcs(env.FS, uid(), cache)
	if _, ok := cache[700]; ok || len(ps) != 15 {
		t.Fatalf("700 still cached (%d processes)", len(ps))
	}
}

func TestStartOf(t *testing.T) {
	env := source.Fixture(t, "apps")
	if s := startOf(env.FS, 601); s != 610 {
		t.Fatalf("start = %d", s)
	}
	if s := startOf(env.FS, 12345); s != 0 {
		t.Fatalf("missing process start = %d", s)
	}
}
