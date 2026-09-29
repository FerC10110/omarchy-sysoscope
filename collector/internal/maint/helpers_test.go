package maint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// tempEnv es un Env vacío sobre un directorio temporal: root/ para el FS,
// cmd/ para el FakeRunner y un FakeBus sin respuestas.
func tempEnv(t *testing.T) (source.Env, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "root"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "cmd"), 0o755); err != nil {
		t.Fatal(err)
	}
	return source.Env{
		FS:    source.FS{Root: filepath.Join(dir, "root")},
		Run:   &source.FakeRunner{Dir: filepath.Join(dir, "cmd")},
		Bus:   &source.FakeBus{System: map[string]json.RawMessage{}},
		Now:   func() time.Time { return source.FixtureNow },
		Lang:  "en",
		Home:  "/home/user",
		State: t.TempDir(),
		Cache: t.TempDir(),
		User:  source.FixtureUser,
	}, dir
}

// put escribe dir/rel con s, creando los directorios.
func put(t *testing.T, dir, rel, s string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runner(env source.Env) *source.FakeRunner { return env.Run.(*source.FakeRunner) }

// tempEnvAt es tempEnv sobre un directorio que ya existe (con su root/ y
// su cmd/).
func tempEnvAt(t *testing.T, dir string) (source.Env, string) {
	t.Helper()
	env, _ := tempEnv(t)
	env.FS = source.FS{Root: filepath.Join(dir, "root")}
	env.Run = &source.FakeRunner{Dir: filepath.Join(dir, "cmd")}
	return env, dir
}

func removeFile(dir, rel string) error { return os.Remove(filepath.Join(dir, rel)) }
