package gpu

import (
	"os"
	"path/filepath"
	"testing"
)

// copyWithout copia un fixture a un directorio temporal sin la ruta rel.
func copyWithout(t *testing.T, root, rel string) string {
	t.Helper()
	dst := t.TempDir()
	if err := os.CopyFS(dst, os.DirFS(root)); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(filepath.Join(dst, rel))
	return dst
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
