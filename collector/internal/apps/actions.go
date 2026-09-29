package apps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/actlog"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// Spec es una acción de la sección Aplicaciones. El colector la corre (Run)
// y la vista la ofrece (view/apps.go) desde la misma lista.
type Spec struct {
	Key     string
	ID      string
	Label   func(lang string) string
	Confirm func(lang string) string // nil: sin confirmación
	Input   string                   // "", "workspace", "monitor" o "size"
	Window  bool                     // necesita una ventana
	Guarded bool                     // se rechaza sobre una app protegida
}

// Specs, en el orden de la spec §5. El shell entrega x y X como "borrar":
// forzar el cierre usa K.
var Specs = []Spec{
	{Key: "enter", ID: "window.focus", Window: true, Label: func(l string) string { return i18n.T(l, "Go to window") }},
	{Key: "t", ID: "window.float", Window: true, Label: func(l string) string { return i18n.T(l, "Toggle floating") }},
	{Key: "p", ID: "window.pin", Window: true, Label: func(l string) string { return i18n.T(l, "Pin") }},
	{Key: "f", ID: "window.fullscreen", Window: true, Label: func(l string) string { return i18n.T(l, "Fullscreen") }},
	{Key: "m", ID: "window.maximize", Window: true, Label: func(l string) string { return i18n.T(l, "Maximize") }},
	{Key: "w", ID: "window.to_workspace", Window: true, Input: "workspace", Label: func(l string) string { return i18n.T(l, "Move to workspace") }},
	{Key: "W", ID: "window.to_workspace_silent", Window: true, Input: "workspace", Label: func(l string) string { return i18n.T(l, "Send to workspace") }},
	{Key: "s", ID: "window.special", Window: true, Label: func(l string) string { return i18n.T(l, "Hide / bring back") }},
	{Key: "o", ID: "window.to_monitor", Window: true, Input: "monitor", Label: func(l string) string { return i18n.T(l, "Move to monitor") }},
	{Key: "c", ID: "window.center", Window: true, Label: func(l string) string { return i18n.T(l, "Center") }},
	{Key: "z", ID: "window.resize", Window: true, Input: "size", Label: func(l string) string { return i18n.T(l, "Resize") }},
	{Key: "u", ID: "window.raise", Window: true, Label: func(l string) string { return i18n.T(l, "Bring to front") }},
	{Key: "a", ID: "window.group", Window: true, Label: func(l string) string { return i18n.T(l, "Group") }},
	{Key: "b", ID: "window.opaque", Window: true, Label: func(l string) string { return i18n.T(l, "Opaque") }},
	{Key: "i", ID: "window.inhibit_idle", Window: true, Label: func(l string) string { return i18n.T(l, "Keep screen on") }},
	{Key: "x", ID: "window.close", Window: true, Label: func(l string) string { return i18n.T(l, "Close") },
		Confirm: func(l string) string { return i18n.T(l, "Close this window?") }},
	{Key: "K", ID: "app.kill", Guarded: true, Label: func(l string) string { return i18n.T(l, "Force close app") },
		Confirm: func(l string) string { return i18n.T(l, "Force-close the whole app?") }},
	{Key: "F", ID: "app.freeze", Guarded: true, Label: func(l string) string { return i18n.T(l, "Freeze / resume") },
		Confirm: func(l string) string { return i18n.T(l, "Freeze or resume the whole app?") }},
	{Key: "B", ID: "app.btop", Label: func(l string) string { return i18n.T(l, "Open in btop") }},
	{Key: "L", ID: "window.capture", Window: true, Label: func(l string) string { return i18n.T(l, "Capture") }},
}

var getenv = os.Getenv

// hyprMessage es el prefijo de los avisos de Hyprland ("info: =[C]:-1: ").
var hyprMessage = regexp.MustCompile(`^\w+: =\[C\]:-?\d+: `)

type target struct {
	app group
	win *winRef
	hy  *hyprState
}

func (t *target) name() string {
	if t.win != nil && t.win.w.Title != "" {
		return t.win.w.Title
	}
	return t.app.name
}

// Run corre la acción id sobre target: una ventana ("0x…" o
// "0x…:<stableId>") o una app sin ventana ("app:<ID>"). input es el dato
// que pidió (workspace, monitor, tamaño). Devuelve el texto para la barra
// de estado. Cada acción queda en <State>/actions.log con su hora y su
// resultado.
func Run(env source.Env, id, target, input string) (string, error) {
	out, err := runAction(env, id, target, input)
	actlog.Write(env, id, target, input, out, err)
	return out, err
}

func runAction(env source.Env, id, target, input string) (string, error) {
	l := env.Lang
	i := slices.IndexFunc(Specs, func(s Spec) bool { return s.ID == id })
	if i < 0 {
		return "", errors.New(i18n.F(l, "unknown action %q", id))
	}
	sp := Specs[i]
	t, err := resolve(env, target)
	if err != nil {
		return "", err
	}
	if sp.Window && t.win == nil {
		return "", errors.New(i18n.T(l, "this app has no window"))
	}
	if sp.Guarded && t.app.protected {
		return "", errors.New(i18n.F(l, "%s keeps the session running: Sysoscope does not freeze or close it", t.app.name))
	}
	return run(env, sp, t, input)
}

// resolve vuelve a leer procesos y ventanas: la fila pudo cambiar desde la
// muestra, y Hyprland acepta sin quejarse una dirección que ya no existe.
func resolve(env source.Env, tg string) (*target, error) {
	l := env.Lang
	hy, herr := readHypr(env.Hypr)
	wins := map[int]string{}
	if herr == nil {
		wins = hy.classes()
	}
	ps := readProcs(env.FS, uint32(os.Getuid()), map[int]meta{})
	gs := groupProcs(ps, wins, SelfChain(env.FS, os.Getpid()))
	if id, ok := strings.CutPrefix(tg, "app:"); ok {
		for _, g := range gs {
			if g.id == id {
				return &target{app: g, hy: hy}, nil
			}
		}
		return nil, errors.New(i18n.T(l, "the app is no longer running"))
	}
	if herr != nil {
		return nil, errors.New(i18n.T(l, "Hyprland is not running"))
	}
	// Una fila de ventana es "<dirección>:<stableId>" (o solo la dirección):
	// Hyprland reusa direcciones, y la misma dirección con otro stableId es
	// otra ventana.
	addr, sid, _ := strings.Cut(tg, ":")
	w, ok := hy.byAddr[addr]
	if !ok || (sid != "" && w.w.StableID != sid) {
		return nil, errors.New(i18n.T(l, "the window no longer exists"))
	}
	for _, g := range gs {
		for _, p := range g.procs {
			if p.pid == w.pid {
				return &target{app: g, win: &w, hy: hy}, nil
			}
		}
	}
	// La ventana es de un proceso ajeno: se puede mover, no matar ni congelar.
	return &target{app: group{name: w.w.Class, protected: true}, win: &w, hy: hy}, nil
}

func run(env source.Env, sp Spec, t *target, input string) (string, error) {
	l := env.Lang
	var exprs []string
	switch sp.ID {
	case "app.kill":
		return kill(env, t.app)
	case "app.freeze":
		f := LoadFreezer(env)
		if f.Resume(env, t.app.id) {
			return i18n.F(l, "%s was resumed", t.app.name), nil
		}
		if err := f.freeze(env, t.app, env.Now()); err != nil {
			return "", err
		}
		return i18n.F(l, "%s is frozen for 5 minutes", t.app.name), nil
	case "window.capture":
		return capture(env, t)
	case "app.btop":
		if len(t.app.procs) == 0 {
			return "", errors.New(i18n.T(l, "the app is no longer running"))
		}
		exprs = []string{`hl.dsp.exec_cmd("xdg-terminal-exec btop --filter ` + safe(btopFilter(t)) + `")`}
	default:
		var err error
		if exprs, err = windowExprs(env, sp, t, input); err != nil {
			return "", err
		}
	}
	if env.Hypr == nil {
		return "", errors.New(i18n.T(l, "Hyprland is not running"))
	}
	for _, e := range exprs {
		out, err := env.Hypr.Request("dispatch " + e)
		if err != nil {
			return "", err
		}
		if s := strings.TrimSpace(string(out)); s != "ok" {
			return "", errors.New(hyprMessage.ReplaceAllString(s, ""))
		}
	}
	if sp.ID == "window.focus" || sp.ID == "window.to_workspace" {
		if a, err := activeWindow(env.Hypr); err == nil && a != t.win.w.Address {
			if sp.ID == "window.to_workspace" {
				return "", errors.New(i18n.F(l, "%s moved to workspace %s, but the keyboard focus did not follow it", t.name(), strings.TrimSpace(input)))
			}
			return "", errors.New(i18n.T(l, "the window did not get the keyboard focus: something else is holding it"))
		}
	}
	return i18n.F(l, "%s: %s", sp.Label(l), t.name()), nil
}

// activeWindow es la dirección de la ventana con el foco. Hyprland contesta
// "ok" a hl.dsp.focus aunque no mueva el foco (una sesión bloqueada, una
// capa que retiene el teclado): se comprueba después.
func activeWindow(h source.Hypr) (string, error) {
	b, err := h.Request("j/activewindow")
	if err != nil {
		return "", err
	}
	var w struct {
		Address string `json:"address"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return "", err
	}
	return w.Address, nil
}

// windowExprs son las expresiones Lua de Hyprland 0.56 para una acción de ventana.
func windowExprs(env source.Env, sp Spec, t *target, input string) ([]string, error) {
	l := env.Lang
	w := t.win.w
	sel := `window="address:` + w.Address + `"`
	float := `hl.dsp.window.float({action="toggle",` + sel + `})`
	center := `hl.dsp.window.center({` + sel + `})`
	switch sp.ID {
	case "window.focus":
		return []string{`hl.dsp.focus({` + sel + `})`}, nil
	case "window.float":
		return []string{float}, nil
	case "window.pin":
		if !w.Floating {
			return nil, errors.New(i18n.T(l, "only a floating window can be pinned"))
		}
		return []string{`hl.dsp.window.pin({` + sel + `})`}, nil
	case "window.fullscreen":
		return []string{`hl.dsp.window.fullscreen({mode="fullscreen",` + sel + `})`}, nil
	case "window.maximize":
		return []string{`hl.dsp.window.fullscreen({mode="maximized",` + sel + `})`}, nil
	case "window.to_workspace", "window.to_workspace_silent":
		n, err := strconv.Atoi(strings.TrimSpace(input))
		if err != nil || n < 1 || n > 99 {
			return nil, errors.New(i18n.T(l, "a workspace from 1 to 99"))
		}
		return []string{fmt.Sprintf(`hl.dsp.window.move({workspace="%d",follow=%t,%s})`, n, sp.ID == "window.to_workspace", sel)}, nil
	case "window.special":
		if !strings.HasPrefix(w.Workspace, "special:") {
			return []string{`hl.dsp.window.move({workspace="special:sysoscope",follow=false,` + sel + `})`}, nil
		}
		ws, err := activeWorkspace(env.Hypr)
		if err != nil {
			return nil, err
		}
		return []string{`hl.dsp.window.move({workspace=` + strconv.Quote(ws) + `,follow=true,` + sel + `})`}, nil
	case "window.to_monitor":
		if _, ok := t.hy.monitor(input); !ok {
			return nil, errors.New(i18n.F(l, "unknown monitor %q", input))
		}
		return []string{`hl.dsp.window.move({monitor=` + strconv.Quote(input) + `,` + sel + `})`}, nil
	case "window.center":
		if w.Floating {
			return []string{center}, nil
		}
		return []string{float, center}, nil
	case "window.resize":
		frac := map[string]float64{"50%": 0.5, "70%": 0.7, "90%": 0.9}[strings.TrimSpace(input)]
		if frac == 0 {
			return nil, errors.New(i18n.T(l, "one of: 50%, 70%, 90%"))
		}
		m, ok := t.hy.monitor(w.Monitor)
		if !ok {
			return nil, errors.New(i18n.F(l, "unknown monitor %q", w.Monitor))
		}
		scale := m.Scale
		if scale <= 0 {
			scale = 1
		}
		x, y := int(float64(m.Width)/scale*frac), int(float64(m.Height)/scale*frac)
		var out []string
		if !w.Floating {
			out = append(out, float)
		}
		return append(out, fmt.Sprintf(`hl.dsp.window.resize({x=%d,y=%d,%s})`, x, y, sel), center), nil
	case "window.raise":
		// bring_to_top no acepta ventana (sube la del foco): alter_zorder sí.
		return []string{`hl.dsp.window.alter_zorder({mode="top",` + sel + `})`}, nil
	case "window.group":
		return []string{`hl.dsp.group.toggle({` + sel + `})`}, nil
	case "window.opaque":
		return []string{`hl.dsp.window.set_prop({prop="opaque",value="toggle",` + sel + `})`}, nil
	case "window.inhibit_idle":
		v := "always"
		if w.InhibitIdle {
			v = "none"
		}
		return []string{`hl.dsp.window.set_prop({prop="idle_inhibit",value="` + v + `",` + sel + `})`}, nil
	case "window.close":
		return []string{`hl.dsp.window.close({` + sel + `})`}, nil
	}
	return nil, errors.New(i18n.F(l, "unknown action %q", sp.ID))
}

func activeWorkspace(h source.Hypr) (string, error) {
	b, err := h.Request("j/activeworkspace")
	if err != nil {
		return "", err
	}
	var ws struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(b, &ws); err != nil {
		return "", err
	}
	return ws.Name, nil
}

// btopFilter es el comm con el que btop encuentra la app: el del proceso de
// su ventana o, sin ventana, el más común de sus procesos (el primero puede
// ser un ayudante o el shell de una terminal).
func btopFilter(t *target) string {
	for _, p := range t.app.procs {
		if t.win != nil && p.pid == t.win.pid {
			return p.comm
		}
		if t.win == nil && t.hy != nil && len(t.hy.windows[p.pid]) > 0 {
			return p.comm
		}
	}
	n := map[string]int{}
	best := t.app.procs[0].comm
	for _, p := range t.app.procs {
		n[p.comm]++
		if n[p.comm] > n[best] {
			best = p.comm
		}
	}
	return best
}

// safe deja letras, números, punto, guion y guion bajo: lo que va dentro de
// un comando o un nombre de archivo.
func safe(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' {
			return r
		}
		return -1
	}, s)
}

// capture guarda la ventana (visible) en la carpeta de capturas de Omarchy.
func capture(env source.Env, t *target) (string, error) {
	l := env.Lang
	w := t.win.w
	if !t.win.visible {
		return "", errors.New(i18n.T(l, "the window is not on screen: go to it first"))
	}
	dir := getenv("OMARCHY_SCREENSHOT_DIR")
	if dir == "" {
		dir = getenv("XDG_PICTURES_DIR")
	}
	if dir == "" {
		dir = filepath.Join(env.Home, "Pictures")
	}
	path := filepath.Join(dir, fmt.Sprintf("sysoscope-%s-%s.png", safe(w.Class), env.Now().Format("20060102-150405")))
	geom := fmt.Sprintf("%d,%d %dx%d", w.At[0], w.At[1], w.Size[0], w.Size[1])
	os.MkdirAll(dir, 0o755)
	if _, err := env.Run.Run(context.Background(), "grim", "-g", geom, path); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", errors.New(i18n.F(l, "requires %s", "grim"))
		}
		return "", err
	}
	return i18n.F(l, "saved to %s", path), nil
}

// kill manda SIGTERM a toda la app y, a los 3 s, SIGKILL a lo que quede.
// Una app congelada se reanuda antes: un proceso detenido no atiende SIGTERM.
func kill(env source.Env, g group) (string, error) {
	l := env.Lang
	LoadFreezer(env).Resume(env, g.id)
	alive := func(p proc) bool { return startOf(env.FS, p.pid) == p.start }
	for _, p := range g.procs {
		if alive(p) {
			sendSignal(p.pid, syscall.SIGTERM)
		}
	}
	for range 30 {
		if !slices.ContainsFunc(g.procs, alive) {
			return i18n.F(l, "%s was closed", g.name), nil
		}
		sleep(100 * time.Millisecond)
	}
	for _, p := range g.procs {
		if alive(p) {
			sendSignal(p.pid, syscall.SIGKILL)
		}
	}
	return i18n.F(l, "%s was force-closed", g.name), nil
}
