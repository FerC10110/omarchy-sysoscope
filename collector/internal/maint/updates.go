package maint

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// UpdatesFile guarda la última comprobación en la caché de Sysoscope, para
// que sobreviva a un reinicio del stream y la vean el panel y lazylazy.
const UpdatesFile = "updates.json"

// CheckTimeout es el tope de una comprobación: checkupdates baja las bases
// de datos de los mirrors.
const CheckTimeout = 60 * time.Second

// updateLine es "nombre vieja -> nueva". Un nombre de paquete de Arch no
// empieza con guion ni punto (así nunca llega a pacman como opción).
var updateLine = regexp.MustCompile(`^([a-z0-9@_+][a-z0-9@._+-]*) (\S+) -> (\S+)$`)

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// CheckupDB es la copia de las bases de datos que usa checkupdates:
// $CHECKUPDATES_DB, si no ${TMPDIR:-/tmp}/checkup-db-<uid>.
func CheckupDB(getenv func(string) string, uid int) string {
	if db := getenv("CHECKUPDATES_DB"); db != "" {
		return filepath.Clean(db)
	}
	tmp := getenv("TMPDIR")
	if tmp == "" {
		tmp = "/tmp"
	}
	return filepath.Join(tmp, "checkup-db-"+strconv.Itoa(uid))
}

// CheckUpdates busca actualizaciones en los repos oficiales con
// checkupdates (pacman-contrib), que baja las bases de los mirrors a su
// propia copia sin tocar la de pacman ni pedir root. El AUR no se consulta
// nunca: mandaría los nombres de los paquetes instalados a
// aur.archlinux.org. db es la copia de checkupdates, de la que sale el
// repositorio de cada paquete (sin red). Nunca devuelve nil.
func CheckUpdates(env source.Env, db string) *model.Updates {
	u := &model.Updates{Checked: env.Now().UTC(), Repo: []model.PkgUpdate{}}
	ctx, cancel := context.WithTimeout(context.Background(), CheckTimeout)
	defer cancel()
	out, err := env.Run.Run(ctx, "checkupdates", "--nocolor")
	switch {
	case err == nil:
	case errors.Is(err, exec.ErrNotFound):
		u.Error = i18n.F(env.Lang, "requires %s", "pacman-contrib")
		return u
	case errors.Is(err, context.DeadlineExceeded):
		u.Error = i18n.T(env.Lang, "timed out")
		return u
	case source.ExitStatus(err) == 2: // no hay pendientes
		return u
	default:
		u.Error = reason(err)
		return u
	}
	var names []string
	for _, l := range strings.Split(string(out), "\n") {
		if m := updateLine.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			u.Repo = append(u.Repo, model.PkgUpdate{Name: m[1], Old: m[2], New: m[3]})
			names = append(names, m[1])
		}
	}
	if len(names) > 0 {
		repos := repoOf(ctx, env, db, names)
		for i := range u.Repo {
			u.Repo[i].Repo = repos[u.Repo[i].Name]
		}
	}
	return u
}

// repoOf pregunta a pacman, sobre la copia de checkupdates y sin red, de
// qué repositorio sale cada paquete. Si falla, un mapa vacío.
func repoOf(ctx context.Context, env source.Env, db string, names []string) map[string]string {
	args := append([]string{"-Sddp", "--print-format", "%r %n", "--dbpath", db}, names...)
	out, err := env.Run.Run(ctx, "pacman", args...)
	m := map[string]string{}
	if err != nil {
		return m
	}
	for _, l := range strings.Split(string(out), "\n") {
		if repo, name, ok := strings.Cut(strings.TrimSpace(l), " "); ok {
			m[name] = repo
		}
	}
	return m
}

// reason es la última línea con texto de stderr (sin colores ni caracteres
// de control), o el error.
func reason(err error) string {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		lines := strings.Split(string(ee.Stderr), "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			if s := clean(lines[i]); s != "" {
				return s
			}
		}
	}
	return clean(err.Error())
}

func clean(s string) string {
	s = ansi.ReplaceAllString(s, "")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

// LoadUpdates lee updates.json. Running y Stale no se guardan: se calculan.
func LoadUpdates(path string) (*model.Updates, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var u model.Updates
	if err := json.Unmarshal(data, &u); err != nil {
		return nil, err
	}
	if u.Checked.IsZero() {
		return nil, errors.New("updates.json: no check time")
	}
	if u.Repo == nil {
		u.Repo = []model.PkgUpdate{}
	}
	u.Running, u.Stale = false, false
	return &u, nil
}

// Stale dice si hubo un cambio de paquetes (pacman.log) después de la
// comprobación: esa lista ya no vale.
func Stale(u *model.Updates, up *model.Upgrade) bool {
	return u != nil && up != nil && !u.Checked.IsZero() && up.LastChange.After(u.Checked)
}
