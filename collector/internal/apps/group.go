package apps

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// group es una aplicación: sus procesos y si se la puede congelar o matar.
type group struct {
	id, name  string
	procs     []proc
	protected bool
}

// protectedComms sostienen la sesión: congelarlos o matarlos la rompe.
// Xwayland, uwsm, waitpid (el bindpid de uwsm) y (sd-pam) son los comm tal
// como se ven en /proc en Omarchy; hyprlock es la pantalla de bloqueo.
var protectedComms = map[string]bool{
	"Hyprland": true, "quickshell": true, "pipewire": true, "pipewire-pulse": true, "wireplumber": true,
	"systemd": true, "dbus-broker": true, "dbus-broker-lau": true, "dbus-daemon": true,
	"Xwayland": true, "uwsm": true, "waitpid": true, "(sd-pam)": true, "hyprlock": true,
}

var sessionScope = regexp.MustCompile(`/session-[0-9]+\.scope(/|$)`)

// inSession dice si el cgroup es parte de la sesión: session.slice del
// usuario (Hyprland, portales, gvfs) o el scope del login (session-N.scope).
// app.slice y background.slice no lo son.
func inSession(cgroup string) bool {
	return strings.Contains(cgroup, "/session.slice/") || sessionScope.MatchString(cgroup)
}

// appScope es el scope de systemd de app.slice donde corre el proceso (lo que
// lanzan Omarchy y uwsm), o "".
func appScope(cgroup string) string {
	if !strings.Contains(cgroup, "/app.slice/") {
		return ""
	}
	last := cgroup[strings.LastIndexByte(cgroup, '/')+1:]
	if !strings.HasSuffix(last, ".scope") {
		return ""
	}
	return last
}

// genericNames son finales de un nombre DNS inverso que no dicen nada
// (com.spotify.Client): se usa la parte anterior.
var genericNames = map[string]bool{"Client": true, "App": true, "Desktop": true, "Application": true}

// Sufijos que systemd, uwsm, Hyprland o tmux le agregan al nombre del scope:
// números (pid, contador), 8 hexadecimales al azar o un UUID.
var (
	uuidSuffix   = regexp.MustCompile(`-[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$`)
	randomSuffix = regexp.MustCompile(`-[0-9a-f]{8}$`)
	numSuffix    = regexp.MustCompile(`(-[0-9]+)+$`)
)

// scopeName saca un nombre legible de un scope:
// "app-org.chromium.Chromium-2765.scope" → "Chromium", "kitty-3689-0.scope" → "kitty",
// `app-Hyprland-xdg\x2dterminal\x2dexec-cc80a9dd.scope` → "xdg-terminal-exec".
// "" si no queda nada legible ("app-Hyprland-12345.scope").
func scopeName(scope string) string {
	s := strings.TrimPrefix(strings.TrimSuffix(scope, ".scope"), "app-")
	if i := strings.IndexByte(s, '@'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimPrefix(strings.TrimPrefix(s, "Hyprland-"), "flatpak-")
	// Los sufijos se sacan antes de desescapar (un "\x2d" no es un
	// separador), con un "-" delante: un nombre que es solo sufijo queda vacío.
	s = numSuffix.ReplaceAllString(uuidSuffix.ReplaceAllString("-"+s, ""), "")
	s = numSuffix.ReplaceAllString(randomSuffix.ReplaceAllString(s, ""), "")
	s = lastName(unescapeUnit(strings.TrimPrefix(s, "-")))
	if digits(s) {
		return ""
	}
	return s
}

// UnitName es el nombre legible de la unidad de una app, un .scope o un
// .service (como los que mata systemd-oomd): "" si no queda nada legible.
func UnitName(unit string) string {
	return scopeName(strings.TrimSuffix(unit, ".service"))
}

// lastName es la parte útil de un nombre DNS inverso: "org.chromium.Chromium"
// → "Chromium", "com.spotify.Client" → "spotify".
func lastName(s string) string {
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return s
	}
	if n := parts[len(parts)-1]; !genericNames[n] {
		return n
	}
	return parts[len(parts)-2]
}

// reverseDNS es una clase como "org.omarchy.btop" o "com.mitchellh.ghostty"
// (no "chrome-www.tradingview.com__chart", la de una web app).
var reverseDNS = regexp.MustCompile(`^[a-z]+(\.[A-Za-z0-9_-]+)+$`)

// className es el nombre de una app que sale de la clase de su ventana: la
// última parte si es DNS inverso, como el scope.
func className(c string) string {
	if reverseDNS.MatchString(c) {
		return lastName(c)
	}
	return c
}

// unescapeUnit deshace el escape de systemd en nombres de unidades y
// cgroups: "\x2d" es "-".
func unescapeUnit(s string) string {
	if !strings.Contains(s, `\x`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) && s[i+1] == 'x' {
			if v, err := strconv.ParseUint(s[i+2:i+4], 16, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func digits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// groupProcs arma las aplicaciones: el scope de la app; si no tiene, el
// ancestro que tiene una ventana; si no, el nombre del ejecutable. self son
// el colector y sus ancestros: su app queda protegida.
//
// Un scope cuyo proceso es hijo de otro scope de app es la misma app:
// Chromium se muda a su propio scope y deja a sus hijos en el que abrió
// Hyprland, y kitty pone cada shell en un scope suyo.
func groupProcs(ps []proc, wins map[int]string, self map[int]bool) []group {
	byPID := make(map[int]*proc, len(ps))
	for i := range ps {
		byPID[ps[i].pid] = &ps[i]
	}
	root := scopeRoots(ps, byPID)
	idx := map[string]int{}
	var out []group
	for _, p := range ps {
		id, name := groupOf(p, byPID, wins, root)
		i, ok := idx[id]
		if !ok {
			i = len(out)
			idx[id] = i
			out = append(out, group{id: id, name: name})
		}
		g := &out[i]
		g.procs = append(g.procs, p)
		if self[p.pid] || protectedComms[p.comm] {
			g.protected = true
		}
	}
	for i := range out {
		if strings.HasSuffix(out[i].id, ".scope") {
			out[i].name = appName(out[i], wins)
		}
		// Sin ventana, algo de la sesión es un servicio de ella: cerrarlo
		// puede terminar la sesión. Con ventana es una app (lanzada desde
		// una TTY, por ejemplo) y se puede cerrar.
		windowed, session := false, false
		for _, p := range out[i].procs {
			_, w := wins[p.pid]
			windowed = windowed || w
			session = session || inSession(p.cgroup)
		}
		if session && !windowed {
			out[i].protected = true
		}
	}
	return out
}

// scopeRoots da, para cada scope de app, el scope de app del que desciende
// (él mismo si no desciende de otro).
func scopeRoots(ps []proc, byPID map[int]*proc) func(string) string {
	parent := map[string]string{}
	find := func(s string) string {
		for n := 0; n < 64; n++ {
			p, ok := parent[s]
			if !ok || p == s {
				return s
			}
			s = p
		}
		return s
	}
	for _, p := range ps {
		s := appScope(p.cgroup)
		q := byPID[p.ppid]
		if s == "" || q == nil {
			continue
		}
		if t := appScope(q.cgroup); t != "" && t != s && sameApp(p, *q, s) {
			// Un ciclo (dos scopes con hijos en el otro) deja el primero.
			if rs, rt := find(s), find(t); rs != rt {
				parent[rs] = rt
			}
		}
	}
	return find
}

// shells no unen scopes: lo que se lanza desde una terminal con uwsm-app
// (omarchy-launch-browser) es otra app, aunque su padre sea el bash de kitty.
var shells = map[string]bool{"bash": true, "zsh": true, "fish": true, "sh": true, "dash": true,
	"nu": true, "ksh": true, "tcsh": true, "csh": true, "xonsh": true, "elvish": true}

// sameApp dice si el scope s de p, hijo de q, es parte de la app de q: el
// mismo programa (Chromium se muda de scope y deja a sus hijos) o el scope
// que el programa abre para sus hijos ("kitty-<pid>-<n>.scope").
func sameApp(p, q proc, s string) bool {
	if shells[q.comm] {
		return false
	}
	if p.comm == q.comm {
		return true
	}
	rest, ok := strings.CutPrefix(strings.TrimSuffix(s, ".scope"), q.comm+"-"+strconv.Itoa(q.pid)+"-")
	return ok && digits(rest)
}

func groupOf(p proc, byPID map[int]*proc, wins map[int]string, root func(string) string) (string, string) {
	if s := appScope(p.cgroup); s != "" {
		s = root(s)
		return s, scopeName(s)
	}
	q := &p
	for n := 0; q != nil && n < 64; n++ {
		if _, ok := wins[q.pid]; ok {
			if s := appScope(q.cgroup); s != "" {
				s = root(s)
				return s, scopeName(s)
			}
			return "comm:" + q.comm, q.comm
		}
		q = byPID[q.ppid]
	}
	return "comm:" + p.comm, p.comm
}

// launchers son programas que lanzan otro dentro de su propio scope (lo
// que Omarchy pasa a uwsm-app): el scope lleva su nombre, no el de la app.
var launchers = map[string]bool{"xdg-terminal-exec": true, "gtk-launch": true}

// appName es el nombre de una app con scope: el del scope; si es un lanzador,
// la clase de su primera ventana o, sin ventana, el comm; si no queda nada
// legible, la clase o el scope entero.
func appName(g group, wins map[int]string) string {
	n := scopeName(g.id)
	if n != "" && !launchers[n] {
		return n
	}
	for _, p := range g.procs {
		if c := wins[p.pid]; c != "" {
			return className(c)
		}
	}
	if n == "" || len(g.procs) == 0 || g.procs[0].comm == "" {
		return unescapeUnit(g.id)
	}
	return g.procs[0].comm
}

// SelfChain son pid y sus ancestros: el colector y quien lo lanzó (el shell,
// lazylazy, su terminal). Congelarlos o matarlos cortaría la rama en la que
// uno está sentado.
func SelfChain(fs source.FS, pid int) map[int]bool {
	out := map[int]bool{}
	for n := 0; pid > 1 && n < 64; n++ {
		out[pid] = true
		s, err := fs.Read("/proc/" + strconv.Itoa(pid) + "/stat")
		if err != nil {
			break
		}
		p, ok := parseStat(s)
		if !ok {
			break
		}
		pid = p.ppid
	}
	return out
}
