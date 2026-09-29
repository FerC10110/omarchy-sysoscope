package apps

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// FreezeFor es cuánto dura un congelamiento antes de reanudarse solo.
const FreezeFor = 5 * time.Minute

// sendSignal y sleep son variables para que los tests no manden señales ni esperen.
var (
	sendSignal = func(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) }
	sleep      = time.Sleep
)

type frozenProc struct {
	PID   int    `json:"pid"`
	Start uint64 `json:"start"`
}

// frozenApp es una app congelada: por cgroup.freeze de su scope (y de los
// otros scopes de app de sus procesos, en Cgroups) o, si no tiene uno propio,
// con SIGSTOP a cada proceso (con su arranque, para no despertar a otro que
// herede el pid).
type frozenApp struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	Until   time.Time    `json:"until"`
	Cgroup  string       `json:"cgroup,omitempty"`
	Cgroups []string     `json:"cgroups,omitempty"`
	Procs   []frozenProc `json:"procs,omitempty"`
}

// cgroups son todos los cgroups congelados de la app.
func (e frozenApp) cgroups() []string {
	if e.Cgroup == "" {
		return e.Cgroups
	}
	return append([]string{e.Cgroup}, e.Cgroups...)
}

// Freezer lleva los congelamientos en <State>/frozen.json: un colector que
// arranca (o el de lazylazy) sabe qué reanudar. Los cambios se hacen con
// <State>/frozen.lock tomado, porque puede haber más de un colector.
type Freezer struct {
	path string
	list []frozenApp
	mod  time.Time
	size int64
}

func LoadFreezer(env source.Env) *Freezer {
	f := &Freezer{path: filepath.Join(env.State, "frozen.json")}
	f.reload(false)
	return f
}

// reload relee el archivo si cambió su hora o su tamaño (o siempre, con force).
func (f *Freezer) reload(force bool) {
	st, err := os.Stat(f.path)
	if err != nil {
		f.list, f.mod, f.size = nil, time.Time{}, 0
		return
	}
	if !force && st.ModTime().Equal(f.mod) && st.Size() == f.size {
		return
	}
	b, err := os.ReadFile(f.path)
	if err != nil {
		return
	}
	var l []frozenApp
	if json.Unmarshal(b, &l) == nil {
		f.list, f.mod, f.size = l, st.ModTime(), st.Size()
	}
}

// lock toma frozen.lock en exclusiva y devuelve cómo soltarlo.
func (f *Freezer) lock() (func(), error) {
	dir := filepath.Dir(f.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	fd, err := os.OpenFile(filepath.Join(dir, "frozen.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(fd.Fd()), syscall.LOCK_EX); err != nil {
		fd.Close()
		return nil, err
	}
	return func() {
		syscall.Flock(int(fd.Fd()), syscall.LOCK_UN)
		fd.Close()
	}, nil
}

// lockOrNot es para reanudar: sin lock, igual se reanuda (mejor que dejar
// algo congelado).
func (f *Freezer) lockOrNot() func() {
	if unlock, err := f.lock(); err == nil {
		return unlock
	}
	return func() {}
}

func (f *Freezer) save() error {
	b, err := json.Marshal(f.list)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(f.path), ".frozen-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp.Name())
		return errors.Join(werr, cerr)
	}
	if err := os.Rename(tmp.Name(), f.path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if st, err := os.Stat(f.path); err == nil {
		f.mod, f.size = st.ModTime(), st.Size()
	}
	return nil
}

// freeze congela g: el cgroup de su scope (el de un proceso que está en él,
// no necesariamente el primero), el de los otros scopes de app de sus
// procesos (Chromium deja a sus hijos en otro) y SIGSTOP a los miembros que
// no están en ninguno, o a todos si no hay scope. Si algo falla, deshace todo.
func (f *Freezer) freeze(env source.Env, g group, now time.Time) error {
	unlock, err := f.lock()
	if err != nil {
		return err
	}
	defer unlock()
	f.reload(true)
	e := frozenApp{ID: g.id, Name: g.name, Until: now.Add(FreezeFor)}
	if strings.HasSuffix(g.id, ".scope") {
		// Primero el scope de la app: queda en Cgroup, como antes.
		ps := slices.Clone(g.procs)
		slices.SortStableFunc(ps, func(a, b proc) int {
			return boolInt(appScope(b.cgroup) == g.id) - boolInt(appScope(a.cgroup) == g.id)
		})
		for _, p := range ps {
			if appScope(p.cgroup) == "" || slices.Contains(e.cgroups(), p.cgroup) {
				continue
			}
			if os.WriteFile(env.FS.Real("/sys/fs/cgroup"+p.cgroup+"/cgroup.freeze"), []byte("1"), 0o644) != nil {
				continue
			}
			if e.Cgroup == "" && appScope(p.cgroup) == g.id {
				e.Cgroup = p.cgroup
			} else {
				e.Cgroups = append(e.Cgroups, p.cgroup)
			}
		}
	}
	for _, p := range g.procs {
		if slices.ContainsFunc(e.cgroups(), func(c string) bool { return p.cgroup == c || strings.HasPrefix(p.cgroup, c+"/") }) {
			continue
		}
		// Murió o su pid es de otro: no se toca.
		if startOf(env.FS, p.pid) != p.start {
			continue
		}
		if err := sendSignal(p.pid, syscall.SIGSTOP); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				continue
			}
			f.thaw(env, e)
			return err
		}
		e.Procs = append(e.Procs, frozenProc{PID: p.pid, Start: p.start})
	}
	f.list = append(f.list, e)
	if err := f.save(); err != nil {
		f.list = f.list[:len(f.list)-1]
		f.thaw(env, e)
		return err
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (f *Freezer) thaw(env source.Env, e frozenApp) {
	for _, c := range e.cgroups() {
		os.WriteFile(env.FS.Real("/sys/fs/cgroup"+c+"/cgroup.freeze"), []byte("0"), 0o644)
	}
	for _, p := range e.Procs {
		if startOf(env.FS, p.PID) == p.Start {
			sendSignal(p.PID, syscall.SIGCONT)
		}
	}
}

// Resume reanuda la app id; false si no estaba congelada.
func (f *Freezer) Resume(env source.Env, id string) bool {
	defer f.lockOrNot()()
	f.reload(true)
	i := slices.IndexFunc(f.list, func(e frozenApp) bool { return e.ID == id })
	if i < 0 {
		return false
	}
	f.thaw(env, f.list[i])
	f.list = slices.Delete(f.list, i, i+1)
	f.save()
	return true
}

// ResumeDue relee el archivo y reanuda lo vencido (también lo que venció con
// el colector apagado), con una notificación. Devuelve los nombres.
func (f *Freezer) ResumeDue(env source.Env, now time.Time) []string {
	f.reload(false)
	if !slices.ContainsFunc(f.list, func(e frozenApp) bool { return !now.Before(e.Until) }) {
		return nil
	}
	names := f.resumeDue(env, now)
	for _, n := range names {
		env.Run.Run(context.Background(), "notify-send", "-a", "Sysoscope", i18n.F(env.Lang, "%s was resumed", n))
	}
	return names
}

// resumeDue hace el trabajo de ResumeDue con el lock tomado y el archivo
// releído: no pisa lo que otro colector agregó.
func (f *Freezer) resumeDue(env source.Env, now time.Time) []string {
	defer f.lockOrNot()()
	f.reload(true)
	var names []string
	kept := f.list[:0:0]
	for _, e := range f.list {
		if now.Before(e.Until) {
			kept = append(kept, e)
			continue
		}
		f.thaw(env, e)
		names = append(names, e.Name)
	}
	if len(names) > 0 {
		f.list = kept
		f.save()
	}
	return names
}

// Frozen y List usan lo último leído (ResumeDue relee en cada muestra).
func (f *Freezer) Frozen(id string) bool {
	return slices.ContainsFunc(f.list, func(e frozenApp) bool { return e.ID == id })
}

func (f *Freezer) List() []model.Frozen {
	out := make([]model.Frozen, 0, len(f.list))
	for _, e := range f.list {
		out = append(out, model.Frozen{ID: e.ID, Name: e.Name, Until: e.Until})
	}
	return out
}
