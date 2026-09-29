// Package source es el único acceso del colector al sistema: archivos bajo una
// raíz (así los tests usan un fixture), comandos externos y D-Bus.
package source

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// FS lee archivos relativos a Root ("" = "/"). Las rutas son absolutas del
// sistema real ("/proc/stat"); FS les antepone Root.
type FS struct{ Root string }

func (f FS) path(p string) string {
	if f.Root == "" {
		return p
	}
	return filepath.Join(f.Root, p)
}

// Real devuelve la ruta en disco (con Root) para quien necesite abrirla directo.
func (f FS) Real(p string) string { return f.path(p) }

// Read devuelve el contenido sin el salto de línea final.
func (f FS) Read(p string) (string, error) {
	b, err := os.ReadFile(f.path(p))
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(b), "\n"), nil
}

// Bytes lee un archivo binario tal cual (Read recorta los '\n' finales).
func (f FS) Bytes(p string) ([]byte, error) { return os.ReadFile(f.path(p)) }

func (f FS) ReadInt(p string) (int64, error) {
	s, err := f.Read(p)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(s), 10, 64)
}

func (f FS) ReadFloat(p string) (float64, error) {
	s, err := f.Read(p)
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}

func (f FS) Lines(p string) ([]string, error) {
	s, err := f.Read(p)
	if err != nil {
		return nil, err
	}
	if s == "" {
		return nil, nil
	}
	return strings.Split(s, "\n"), nil
}

// Dir devuelve los nombres de las entradas, ordenados (incluye symlinks).
func (f FS) Dir(p string) ([]string, error) {
	es, err := os.ReadDir(f.path(p))
	if err != nil {
		return nil, err
	}
	names := make([]string, len(es))
	for i, e := range es {
		names[i] = e.Name()
	}
	sort.Strings(names)
	return names, nil
}

// Glob devuelve rutas sin Root, ordenadas.
func (f FS) Glob(pattern string) ([]string, error) {
	m, err := filepath.Glob(f.path(pattern))
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(m))
	for _, p := range m {
		if f.Root != "" {
			p = "/" + strings.TrimPrefix(strings.TrimPrefix(p, f.Root), "/")
		}
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

// Link lee un enlace. En un fixture (Root) el enlace es un archivo
// "<p>.link" con el destino: el Market no acepta symlinks en un plugin.
func (f FS) Link(p string) (string, error) {
	l, err := os.Readlink(f.path(p))
	if err != nil && f.Root != "" {
		if b, ferr := os.ReadFile(f.path(p) + ".link"); ferr == nil {
			return strings.TrimSpace(string(b)), nil
		}
	}
	return l, err
}

func (f FS) Exists(p string) bool {
	_, err := os.Stat(f.path(p))
	return err == nil
}

// Owner devuelve el UID dueño de p. En /proc/<pid> es el del proceso.
func (f FS) Owner(p string) (uint32, error) {
	st, err := os.Stat(f.path(p))
	if err != nil {
		return 0, err
	}
	s, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New("no owner")
	}
	return s.Uid, nil
}
