package maint

import (
	"errors"
	"io/fs"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// SnapperDest es snapperd en el bus de sistema.
const SnapperDest = "org.opensuse.Snapper"

const (
	snapperPath = "/org/opensuse/Snapper"
	snapperSvc  = "/usr/share/dbus-1/system-services/org.opensuse.Snapper.service"
	snapperDir  = "/etc/snapper/configs"
	snapperList = "/etc/conf.d/snapper"
	snapsShown  = 10
)

// RootBtrfs dice si / está en btrfs (snapper solo tiene sentido ahí).
func RootBtrfs(st *model.Storage) bool {
	if st == nil {
		return false
	}
	for _, f := range st.Filesystems {
		if f.FSType == "btrfs" && slices.Contains(f.Mounts, "/") {
			return true
		}
	}
	return false
}

// Snapper lee los configs de snapper (legibles por todos) y deduce de ellos
// si el usuario puede ver los snapshots: root, en ALLOW_USERS o en un grupo
// de ALLOW_GROUPS. Solo en ese caso le pregunta a snapperd por D-Bus; sin
// permiso no hay ninguna llamada (el demonio no arranca). nil si snapper no
// está o / no es btrfs.
func Snapper(env source.Env, rootBtrfs bool, r *collect.Report) *model.Snapshots {
	if !rootBtrfs || !env.FS.Exists(snapperDir) || !env.FS.Exists(snapperSvc) {
		return nil
	}
	u := env.User
	groups := GroupNames(env, u)
	out := &model.Snapshots{User: u.Name, Configs: []model.SnapConfig{}}
	for _, name := range configNames(env) {
		vars, err := shellVars(env, snapperDir+"/"+name)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				// listado en SNAPPER_CONFIGS pero, por ejemplo, sin permiso
				// de lectura (un config 0600): se avisa, no desaparece solo.
				r.FromErr("maintenance.snapshots", "", err)
			}
			continue // no está: listado en SNAPPER_CONFIGS pero no existe
		}
		c := model.SnapConfig{Name: name, AllowUsers: vars["ALLOW_USERS"]}
		c.Allowed = u.UID == 0 || (u.Name != "" && slices.Contains(strings.Fields(c.AllowUsers), u.Name)) ||
			slices.ContainsFunc(strings.Fields(vars["ALLOW_GROUPS"]), func(g string) bool { return slices.Contains(groups, g) })
		if c.Allowed {
			list, total, err := listSnapshots(env, name)
			if err != nil {
				r.Err("maintenance.snapshots", err)
			} else {
				c.List, c.Total = list, total
			}
		}
		out.Configs = append(out.Configs, c)
	}
	if len(out.Configs) == 0 {
		return nil
	}
	return out
}

// configNames son los de SNAPPER_CONFIGS o, sin ese archivo, los de
// /etc/snapper/configs. Un nombre con barra o vacío no es un config.
func configNames(env source.Env) []string {
	var names []string
	if vars, err := shellVars(env, snapperList); err == nil && strings.TrimSpace(vars["SNAPPER_CONFIGS"]) != "" {
		names = strings.Fields(vars["SNAPPER_CONFIGS"])
	} else {
		names, _ = env.FS.Dir(snapperDir)
	}
	var out []string
	for _, n := range names {
		if n != "" && n != "." && n != ".." && !strings.Contains(n, "/") {
			out = append(out, n)
		}
	}
	return out
}

// shellVars lee CLAVE="valor" de un archivo de shell simple (comillas
// dobles o simples; # comenta la línea).
func shellVars(env source.Env, p string) (map[string]string, error) {
	lines, err := env.FS.Lines(p)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		m[strings.TrimSpace(k)] = v
	}
	return m, nil
}

// GroupNames son los grupos de u en /etc/group: por gid (primario y
// suplementarios) o porque figura como miembro.
func GroupNames(env source.Env, u source.User) []string {
	lines, _ := env.FS.Lines("/etc/group")
	var out []string
	for _, l := range lines {
		f := strings.Split(l, ":")
		if len(f) < 4 || f[0] == "" {
			continue
		}
		gid, err := strconv.ParseUint(f[2], 10, 32)
		byGID := err == nil && slices.Contains(u.GIDs, uint32(gid))
		member := u.Name != "" && slices.Contains(strings.Split(f[3], ","), u.Name)
		if byGID || member {
			out = append(out, f[0])
		}
	}
	return out
}

// listSnapshots llama ListSnapshots(config): a(uquxussa{ss}) = número,
// tipo (0 sola, 1 antes, 2 después), número del "antes", fecha (unix), uid,
// descripción, limpieza y datos. Devuelve las 10 más nuevas y el total,
// sin el 0 ("current").
func listSnapshots(env source.Env, config string) ([]model.Snap, int, error) {
	body, err := env.Bus.Call(true, SnapperDest, snapperPath, SnapperDest+".ListSnapshots", config)
	if err != nil {
		return nil, 0, err
	}
	// shapeErr: la respuesta no tiene la forma de ListSnapshots (body[0] no
	// es una lista, o un ítem no lo es o le faltan campos). No se confunde
	// con "sin snapshots": queda en unavailable, no en una lista vacía.
	shapeErr := errors.New(i18n.T(env.Lang, "unexpected answer from snapperd"))
	if len(body) == 0 {
		return nil, 0, shapeErr
	}
	items, ok := body[0].([]any)
	if !ok {
		return nil, 0, shapeErr
	}
	list := []model.Snap{}
	for _, it := range items {
		f, ok := it.([]any)
		if !ok || len(f) < 7 {
			return nil, 0, shapeErr
		}
		n, ok := number(f[0])
		if !ok {
			return nil, 0, shapeErr
		}
		if n <= 0 {
			continue // snapshot 0 ("current"): no se muestra
		}
		s := model.Snap{Number: uint32(n), Type: "single"}
		if t, ok := number(f[1]); ok {
			s.Type = map[float64]string{0: "single", 1: "pre", 2: "post"}[t]
		}
		if d, ok := number(f[3]); ok && d > 0 {
			s.Date = time.Unix(int64(d), 0).UTC()
		}
		s.Description, _ = f[5].(string)
		s.Cleanup, _ = f[6].(string)
		list = append(list, s)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Number > list[j].Number })
	total := len(list)
	if len(list) > snapsShown {
		list = list[:snapsShown]
	}
	return list, total, nil
}

// number convierte los números de godbus (u, q, x) y los del FakeBus (JSON).
func number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint16:
		return float64(x), true
	case uint32:
		return float64(x), true
	case uint64:
		return float64(x), true
	}
	return 0, false
}
