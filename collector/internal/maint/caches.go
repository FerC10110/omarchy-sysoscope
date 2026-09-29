package maint

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// DirSize es lo que ocupa en disco un directorio. Partial: algo no se pudo
// leer (un download-* de pacman sin permiso) o se llegó al tope.
type DirSize struct {
	SizeB   uint64 `json:"size_b"`
	Partial bool   `json:"partial,omitempty"`
}

// DirSizeFunc mide una caché. path es la ruta del sistema real (sin la
// raíz de un fixture): los tests y los fixtures dan tamaños fijos.
type DirSizeFunc func(path string) DirSize

const (
	WalkMax   = 200000          // entradas por raíz
	SizesFile = "dirsizes.json" // tamaños guardados por la captura de un fixture
)

// Walk suma st_blocks × 512 (lo que ocupa en disco, como du) de root y todo
// lo que tiene adentro, sin seguir enlaces ni cruzar a otro sistema de
// archivos. Un subdirectorio que no se puede leer se saltea. Solo la raíz
// se resuelve si es un symlink (p. ej. ~/.cache/yay a otro disco): si no,
// filepath.WalkDir la trata como una hoja y nunca entra, midiendo 0 B.
func Walk(root string, max int) DirSize {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return DirSize{Partial: true}
	}
	var st unix.Stat_t
	if err := unix.Lstat(resolved, &st); err != nil {
		return DirSize{Partial: true}
	}
	dev := st.Dev
	var out DirSize
	n := 0
	filepath.WalkDir(resolved, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			out.Partial = true
			return nil
		}
		if n++; n > max {
			out.Partial = true
			return fs.SkipAll
		}
		var s unix.Stat_t
		if unix.Lstat(p, &s) != nil {
			out.Partial = true
			return nil
		}
		if s.Dev != dev {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		out.SizeB += uint64(s.Blocks) * 512
		return nil
	})
	return out
}

// LoadDirSizes lee dirsizes.json de un fixture ({ruta: DirSize}).
func LoadDirSizes(file string) (DirSizeFunc, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	m := map[string]DirSize{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return func(p string) DirSize { return m[p] }, nil
}

// CacheRoot es una caché que se mide recorriéndola.
type CacheRoot struct{ ID, Path string }

// CacheRoots son las cachés que existen en esta máquina, en el orden de la
// vista: pacman, yay, paru, coredump (el journal se pregunta aparte).
func CacheRoots(env source.Env) []CacheRoot {
	var out []CacheRoot
	for _, c := range []CacheRoot{
		{"pacman", PacmanCacheDir(env)},
		{"yay", env.Home + "/.cache/yay"},
		{"paru", env.Home + "/.cache/paru"},
		{"coredump", "/var/lib/systemd/coredump"},
	} {
		if env.FS.Exists(c.Path) {
			out = append(out, c)
		}
	}
	return out
}

// PacmanCacheDir es el primer CacheDir de [options] en /etc/pacman.conf, o
// el de siempre.
func PacmanCacheDir(env source.Env) string {
	lines, _ := env.FS.Lines("/etc/pacman.conf")
	section := ""
	for _, l := range lines {
		if i := strings.IndexByte(l, '#'); i >= 0 {
			l = l[:i]
		}
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "[") && strings.HasSuffix(l, "]") {
			section = l
			continue
		}
		k, v, ok := strings.Cut(l, "=")
		if !ok || section != "[options]" || strings.TrimSpace(k) != "CacheDir" {
			continue
		}
		if f := strings.Fields(v); len(f) > 0 && filepath.IsAbs(f[0]) {
			return filepath.Clean(f[0])
		}
	}
	return "/var/cache/pacman/pkg"
}

// Caches mide cada caché con size y pregunta el journal a journalctl. El
// orden es el de la vista: pacman, yay, paru, journal, coredump.
func Caches(env source.Env, size DirSizeFunc, r *collect.Report) []model.CacheDir {
	j, jok := journal(env, r)
	var out []model.CacheDir
	for _, c := range CacheRoots(env) {
		if c.ID == "coredump" && jok {
			out, jok = append(out, j), false
		}
		d := size(c.Path)
		out = append(out, model.CacheDir{ID: c.ID, Path: c.Path, SizeB: d.SizeB, Partial: d.Partial})
	}
	if jok {
		out = append(out, j)
	}
	return out
}

// journal corre journalctl --disk-usage (unos 6 ms).
func journal(env source.Env, r *collect.Report) (model.CacheDir, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := env.Run.Run(ctx, "journalctl", "--disk-usage")
	if err != nil {
		r.FromErr("maintenance.caches.journal", "journalctl", err)
		return model.CacheDir{}, false
	}
	b, ok := diskUsage(string(out))
	if !ok {
		r.Err("maintenance.caches.journal", errors.New(clean(string(out))))
		return model.CacheDir{}, false
	}
	return model.CacheDir{ID: "journal", SizeB: b}, true
}

var takeUp = regexp.MustCompile(`take up ([0-9]+(?:\.[0-9]+)?)([BKMGTPE]?)\b`)

// diskUsage entiende "… take up 348.1M …" (unidades de 1024, como
// journalctl).
func diskUsage(s string) (uint64, bool) {
	m := takeUp.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	exp := strings.Index("BKMGTPE", m[2])
	if m[2] == "" {
		exp = 0
	}
	return uint64(math.Round(v * math.Pow(1024, float64(exp)))), true
}
