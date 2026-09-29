package wifi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// tempWifi es el fixture wifi copiado a un directorio temporal: los tests que
// cambian una salida o un archivo de /sys lo hacen sobre la copia.
func tempWifi(t *testing.T) source.Env {
	t.Helper()
	env := source.Fixture(t, "wifi")
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Dir(env.FS.Root))); err != nil {
		t.Fatal(err)
	}
	env.FS.Root = filepath.Join(dir, "root")
	env.Run = &source.FakeRunner{Dir: filepath.Join(dir, "cmd")}
	return env
}

// put escribe un archivo de la copia: rel es "cmd/…" o "root/…".
func put(t *testing.T, env source.Env, rel, content string) {
	t.Helper()
	p := filepath.Join(filepath.Dir(env.FS.Root), rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runs(env source.Env) *source.FakeRunner { return env.Run.(*source.FakeRunner) }
