package maint

import (
	"bytes"
	"encoding/json"
	"path"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// CoredumpArgs pide los volcados de los últimos 7 días en JSON.
var CoredumpArgs = []string{"--json=short", "--no-pager", "list", "--since=-7d"}

const crashesShown = 10

// coredump es un elemento de coredumpctl --json=short (size puede ser null).
type coredump struct {
	Time     int64  `json:"time"` // microsegundos
	PID      int    `json:"pid"`
	UID      uint32 `json:"uid"`
	Sig      int    `json:"sig"`
	Corefile string `json:"corefile"` // present|missing|inaccessible|journal|none…
	Exe      string `json:"exe"`
	Size     *int64 `json:"size"`
}

// ReadCrashes lee los programas que se cerraron en los últimos 7 días
// (coredumpctl, unos 17 ms). Sin volcados, una lista vacía; si falla, nil y
// el unavailable maintenance.health.crashes.
func ReadCrashes(env source.Env, r *collect.Report) *model.Crashes {
	out, err := runHealth(env, "coredumpctl", CoredumpArgs)
	switch {
	case NoEntries(out, err):
		return &model.Crashes{List: []model.Crash{}}
	case err != nil:
		healthErr(r, "maintenance.health.crashes", "coredumpctl", err)
		return nil
	}
	c, perr := parseCrashes(out, UserNames(env), env.Lang)
	if perr != nil {
		r.Err("maintenance.health.crashes", perr)
		return nil
	}
	return c
}

// parseCrashes ordena los volcados del más nuevo al más viejo y deja los 10
// primeros, con el total.
func parseCrashes(out []byte, users map[uint32]string, lang string) (*model.Crashes, error) {
	c := &model.Crashes{List: []model.Crash{}}
	if len(bytes.TrimSpace(out)) == 0 {
		return c, nil
	}
	var ds []coredump
	if err := json.Unmarshal(out, &ds); err != nil {
		return nil, err
	}
	sort.SliceStable(ds, func(i, j int) bool { return ds[i].Time > ds[j].Time })
	c.Total = len(ds)
	for _, d := range ds[:min(len(ds), crashesShown)] {
		prog := i18n.T(lang, "Unknown")
		if d.Exe != "" {
			prog = path.Base(d.Exe)
		}
		user, ok := users[d.UID]
		if !ok {
			user = strconv.FormatUint(uint64(d.UID), 10)
		}
		c.List = append(c.List, model.Crash{Time: time.UnixMicro(d.Time).UTC(), PID: d.PID, Program: prog, Signal: d.Sig,
			SignalName: unix.SignalName(syscall.Signal(d.Sig)), UID: d.UID, User: user,
			Stored: d.Corefile == "present" || d.Corefile == "journal"})
	}
	return c, nil
}

// UserNames es uid → nombre de /etc/passwd (legible por todos), con el
// usuario del Env siempre (la mayoría de los fixtures no traen passwd;
// threadripper sí, para los dueños de los puertos que escuchan).
func UserNames(env source.Env) map[uint32]string {
	m := map[uint32]string{}
	lines, _ := env.FS.Lines("/etc/passwd")
	for _, l := range lines {
		f := strings.Split(l, ":")
		if len(f) < 3 || f[0] == "" {
			continue
		}
		if uid, err := strconv.ParseUint(f[2], 10, 32); err == nil {
			if _, dup := m[uint32(uid)]; !dup {
				m[uint32(uid)] = f[0]
			}
		}
	}
	if env.User.Name != "" {
		m[env.User.UID] = env.User.Name
	}
	return m
}
