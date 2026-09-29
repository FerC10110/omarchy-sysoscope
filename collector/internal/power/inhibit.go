package power

import (
	"encoding/json"
	"errors"
	"math"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/text"
)

// logind y el archivo de "mantener despierto" de Omarchy (omarchy-toggle-idle;
// Omarchy ya no usa hypridle). La captura de fixtures usa los mismos nombres.
const (
	Login1         = "org.freedesktop.login1"
	Login1Path     = "/org/freedesktop/login1"
	ListInhibitors = "org.freedesktop.login1.Manager.ListInhibitors"
	StayAwakeFile  = "/.local/state/omarchy/indicators/stay-awake"
)

var errNoLogind = errors.New("logind is not running")

// Inhibit lee qué impide suspender o apagar la pantalla: los inhibidores de
// logind (preguntando antes NameHasOwner, como con BlueZ), las ventanas de
// Hyprland con inhibitingIdle y el "mantener despierto" de Omarchy. Solo con
// Energía abierta: una llamada D-Bus, un pedido al socket y un stat.
func Inhibit(env source.Env, r *collect.Report) *model.Inhibit {
	in := &model.Inhibit{Logind: []model.Inhibitor{}, Windows: []model.IdleWindow{}}
	in.StayAwake = env.Home != "" && env.FS.Exists(env.Home+StayAwakeFile)
	list, err := inhibitors(env)
	switch {
	case errors.Is(err, errNoLogind):
		r.Absent("power.inhibitors")
	case err != nil:
		r.Err("power.inhibitors", err)
	default:
		in.Logind = list
	}
	ws, err := idleWindows(env.Hypr)
	switch {
	case errors.Is(err, source.ErrNoHyprland):
	case err != nil:
		r.Err("power.idle_windows", err)
	default:
		in.Windows = ws
	}
	return in
}

// inhibitors llama a ListInhibitors, a(ssssuu): qué ("sleep:idle:…"), quién,
// por qué, modo, uid y pid. Lo que no tiene esa forma se descarta. Antes
// pregunta NameHasOwner: si logind no tiene dueño (owner == false), no está
// disponible (not_present); si la pregunta misma falla —el bus no responde—
// es un error, no una ausencia.
func inhibitors(env source.Env) ([]model.Inhibitor, error) {
	if env.Bus == nil {
		return nil, errNoLogind
	}
	owner, err := env.Bus.Call(true, "org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus.NameHasOwner", Login1)
	if err != nil {
		return nil, err
	}
	if len(owner) == 0 {
		return nil, errNoLogind
	}
	if on, ok := owner[0].(bool); !ok || !on {
		return nil, errNoLogind
	}
	body, err := env.Bus.Call(true, Login1, Login1Path, ListInhibitors)
	if err != nil {
		return nil, err
	}
	out := []model.Inhibitor{}
	if len(body) == 0 {
		return out, nil
	}
	list, _ := body[0].([]any)
	for _, it := range list {
		f, ok := it.([]any)
		if !ok || len(f) < 6 {
			continue
		}
		what, _ := f[0].(string)
		who, _ := f[1].(string)
		why, _ := f[2].(string)
		mode, _ := f[3].(string)
		in := model.Inhibitor{Who: clean(who), Why: clean(why), Mode: strings.TrimSpace(mode), UID: u32(f[4]), PID: u32(f[5])}
		for _, w := range strings.Split(what, ":") {
			if w = strings.TrimSpace(w); w != "" {
				in.What = append(in.What, w)
			}
		}
		if len(in.What) == 0 || in.Mode == "" {
			continue
		}
		out = append(out, in)
	}
	return out, nil
}

// u32 lee un uint32 del bus (godbus da uint32; el JSON de un fixture,
// float64). Cualquier otra cosa, o fuera de rango, es 0.
func u32(v any) uint32 {
	switch x := v.(type) {
	case uint32:
		return x
	case float64:
		if x >= 0 && x <= math.MaxUint32 && x == math.Trunc(x) {
			return uint32(x)
		}
	}
	return 0
}

// clean saca las secuencias de escape ANSI/CSI y los caracteres de control
// (los textos los escribe cada programa) y los espacios de los bordes, y
// recorta a 120 runas.
func clean(s string) string {
	s = strings.TrimSpace(text.Clean(s))
	if r := []rune(s); len(r) > 120 {
		s = string(r[:120]) + "…"
	}
	return s
}

// idleWindows pide j/clients y devuelve las ventanas mapeadas con
// inhibitingIdle (la regla idle_inhibit que pone la acción i de
// Aplicaciones, o la que pide la app, p. ej. un video a pantalla completa).
func idleWindows(h source.Hypr) ([]model.IdleWindow, error) {
	if h == nil {
		return nil, source.ErrNoHyprland
	}
	b, err := h.Request("j/clients")
	if err != nil {
		return nil, err
	}
	var cs []struct {
		Class          string `json:"class"`
		Title          string `json:"title"`
		Mapped         bool   `json:"mapped"`
		InhibitingIdle bool   `json:"inhibitingIdle"`
	}
	if err := json.Unmarshal(b, &cs); err != nil {
		return nil, err
	}
	out := []model.IdleWindow{}
	for _, c := range cs {
		if c.Mapped && c.InhibitingIdle {
			out = append(out, model.IdleWindow{Class: clean(c.Class), Title: clean(c.Title)})
		}
	}
	return out, nil
}
