package security

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// fsWith arma un FS sobre un directorio temporal con los archivos dados
// (ruta absoluta del sistema → contenido).
func fsWith(t *testing.T, files map[string]string) source.FS {
	t.Helper()
	root := t.TempDir()
	for p, s := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return source.FS{Root: root}
}
