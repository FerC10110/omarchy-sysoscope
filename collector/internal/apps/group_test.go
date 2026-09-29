package apps

import (
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

func TestScopeName(t *testing.T) {
	for in, want := range map[string]string{
		"app-org.chromium.Chromium-2765.scope":     "Chromium",
		"kitty-3689-0.scope":                       "kitty",
		"app-Hyprland-firefox-1234.scope":          "firefox",
		"app-flatpak-com.spotify.Client-99.scope":  "spotify",
		"app-Hyprland-org.gnome.Nautilus@12.scope": "Nautilus",
		// Sin nada legible: "" (el grupo usa la clase de la ventana o el scope entero).
		"app-1234.scope":              "",
		"app-Hyprland-12345.scope":    "",
		"app-Hyprland-1a2b3c4d.scope": "",
		// systemd escapa "-" como \x2d; uwsm y Hyprland agregan un sufijo al azar.
		`app-Hyprland-xdg\x2dterminal\x2dexec-cc80a9dd.scope`:                 "xdg-terminal-exec",
		`app-Hyprland-omarchy\x2dhyprland\x2dmonitor\x2dwatch-51f72997.scope`: "omarchy-hyprland-monitor-watch",
		"app-Hyprland-chromium-db5eb250.scope":                                "chromium",
		"tmux-spawn-607a3cc5-1b2c-4d5e-8f90-1234567890ab.scope":               "tmux-spawn",
	} {
		if got := scopeName(in); got != want {
			t.Errorf("scopeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUnitName(t *testing.T) {
	for in, want := range map[string]string{
		"app-org.mozilla.firefox-4242.scope":                  "firefox",
		"app-Hyprland-kitty@3f2a.service":                     "kitty",
		"foo.service":                                         "foo",
		"app-Hyprland-12345.scope":                            "",
		`app-Hyprland-xdg\x2dterminal\x2dexec-cc80a9dd.scope`: "xdg-terminal-exec",
	} {
		if got := UnitName(in); got != want {
			t.Errorf("UnitName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAppScope(t *testing.T) {
	u := "/user.slice/user-1000.slice/user@1000.service"
	for in, want := range map[string]string{
		u + "/app.slice/app-org.chromium.Chromium-200.scope": "app-org.chromium.Chromium-200.scope",
		u + "/app.slice/app-graphical.slice/kitty-3-0.scope": "kitty-3-0.scope",
		u + "/app.slice/syncthing.service":                   "",
		"/user.slice/user-1000.slice/session-2.scope":        "",
		"": "",
	} {
		if got := appScope(in); got != want {
			t.Errorf("appScope(%q) = %q, want %q", in, got, want)
		}
	}
}

func groupsOf(t *testing.T, self map[int]bool) map[string]group {
	t.Helper()
	env := source.Fixture(t, "apps")
	ps := readProcs(env.FS, uid(), map[int]meta{})
	out := map[string]group{}
	for _, g := range groupProcs(ps, fixtureWins, self) {
		out[g.id] = g
	}
	return out
}

func TestGroupProcs(t *testing.T) {
	gs := groupsOf(t, nil)
	if len(gs) != 10 {
		t.Fatalf("%d groups: %v", len(gs), gs)
	}
	for id, want := range map[string]struct {
		name      string
		procs     int
		protected bool
	}{
		// 202 y 203 están en otro scope (el que abrió Hyprland) pero son hijos
		// del navegador, que se movió a su propio scope: son la misma app.
		"app-org.chromium.Chromium-200.scope": {"Chromium", 4, false},
		"kitty-300-0.scope":                   {"kitty", 2, false},
		// Lanzado desde el bash de kitty con uwsm-app: es otra app.
		"app-Hyprland-firefox-5f3a9c21.scope": {"firefox", 1, false},
		// /usr/bin/obsidian ejecuta electron43: el nombre es el del scope.
		"app-Hyprland-obsidian-9e8d7c6b.scope": {"obsidian", 2, false},
		// En session-2.scope pero con ventana: se puede cerrar.
		"comm:steam": {"steam", 2, false}, // steamwebhelper, por su ancestro con ventana
		// Sin ventana en session-2.scope (la sesión de login): protegida.
		"comm:Web Content": {"Web Content", 1, true},
		"comm:Hyprland":    {"Hyprland", 1, true},
		"comm:quickshell":  {"quickshell", 1, true},
		"comm:pipewire":    {"pipewire", 1, true},
		"comm:syncthing":   {"syncthing", 1, false},
	} {
		g, ok := gs[id]
		if !ok || g.name != want.name || len(g.procs) != want.procs || g.protected != want.protected {
			t.Errorf("%s = %+v (found %v), want %+v", id, g, ok, want)
		}
	}
}

func TestGroupSelfIsProtected(t *testing.T) {
	if g := groupsOf(t, map[int]bool{301: true})["kitty-300-0.scope"]; !g.protected {
		t.Fatal("the terminal running Sysoscope must be protected")
	}
}

func TestSelfChain(t *testing.T) {
	env := source.Fixture(t, "apps")
	got := SelfChain(env.FS, 301)
	if len(got) != 2 || !got[301] || !got[300] {
		t.Fatalf("chain = %v", got)
	}
	if got := SelfChain(env.FS, 99999); len(got) != 1 || !got[99999] {
		t.Fatalf("a pid outside /proc = %v", got)
	}
}

// Un lanzador (xdg-terminal-exec) le da su nombre al scope de la terminal:
// la app se llama como la clase de su ventana y los shells, que kitty pone
// cada uno en un scope "kitty-<pid>-<n>", son parte de ella. Lo que se lanza
// desde un shell con uwsm-app es otra app.
func TestGroupLauncherScope(t *testing.T) {
	u := "/user.slice/user-1000.slice/user@1000.service/app.slice/app-graphical.slice/"
	xte := u + `app-Hyprland-xdg\x2dterminal\x2dexec-cc80a9dd.scope`
	ps := []proc{
		{pkey: pkey{pid: 10}, ppid: 1, comm: "kitty", cgroup: xte},
		{pkey: pkey{pid: 11}, ppid: 10, comm: "kitten", cgroup: xte},
		{pkey: pkey{pid: 12}, ppid: 10, comm: "bash", cgroup: u + "kitty-10-0.scope"},
		{pkey: pkey{pid: 13}, ppid: 12, comm: "nvim", cgroup: u + "kitty-10-0.scope"},
		{pkey: pkey{pid: 14}, ppid: 12, comm: "chromium", cgroup: u + "app-Hyprland-chromium-1f2e3d4c.scope"},
		{pkey: pkey{pid: 15}, ppid: 14, comm: "chromium", cgroup: u + "app-Hyprland-chromium-1f2e3d4c.scope"},
		{pkey: pkey{pid: 20}, ppid: 1, comm: "hyprsunset", cgroup: u + "app-Hyprland-hyprsunset-031a187f.scope"},
		{pkey: pkey{pid: 30}, ppid: 1, comm: "foot", cgroup: u + `app-Hyprland-xdg\x2dterminal\x2dexec-0badcafe.scope`},
		{pkey: pkey{pid: 40}, ppid: 1, comm: "tool", cgroup: u + "app-Hyprland-5a6b7c8d.scope"},
		{pkey: pkey{pid: 50}, ppid: 1, comm: "x", cgroup: u + "app-Hyprland-12345.scope"},
	}
	gs := map[string]group{}
	// Un lanzador más (gtk-launch) y una clase en DNS inverso: org.omarchy.btop → btop.
	gl := u + `app-Hyprland-gtk\x2dlaunch-77aa88bb.scope`
	ps = append(ps, proc{pkey: pkey{pid: 60}, ppid: 1, comm: "btop", cgroup: gl})
	for _, g := range groupProcs(ps, map[int]string{10: "kitty", 14: "chromium", 40: "Tool", 60: "org.omarchy.btop"}, nil) {
		gs[g.id] = g
	}
	if len(gs) != 7 {
		t.Fatalf("%d groups: %+v", len(gs), gs)
	}
	for id, want := range map[string]struct {
		name  string
		procs int
	}{
		`app-Hyprland-xdg\x2dterminal\x2dexec-cc80a9dd.scope`: {"kitty", 4},
		"app-Hyprland-chromium-1f2e3d4c.scope":                {"chromium", 2},
		"app-Hyprland-hyprsunset-031a187f.scope":              {"hyprsunset", 1},
		`app-Hyprland-xdg\x2dterminal\x2dexec-0badcafe.scope`: {"foot", 1}, // sin ventana: el comm
		"app-Hyprland-5a6b7c8d.scope":                         {"Tool", 1}, // nada legible: la clase
		"app-Hyprland-12345.scope":                            {"app-Hyprland-12345.scope", 1},
		`app-Hyprland-gtk\x2dlaunch-77aa88bb.scope`:           {"btop", 1},
	} {
		if g := gs[id]; g.name != want.name || len(g.procs) != want.procs {
			t.Errorf("%s = %q %d procs, want %+v", id, g.name, len(g.procs), want)
		}
	}
}

// Lo que sostiene la sesión no se congela ni se cierra: los procesos sin
// ventana de session.slice o de un session-N.scope, y por comm Xwayland,
// uwsm, waitpid, (sd-pam) y hyprlock. app.slice y background.slice sí.
func TestGroupSessionIsProtected(t *testing.T) {
	u := "/user.slice/user-1000.slice/user@1000.service"
	ps := []proc{
		{pkey: pkey{pid: 10}, ppid: 1, comm: "gvfsd", cgroup: u + "/session.slice/gvfs-daemon.service"},
		{pkey: pkey{pid: 11}, ppid: 1, comm: "tmux: server", cgroup: u + "/session.slice/wayland-wm@hyprland.desktop.service"},
		{pkey: pkey{pid: 12}, ppid: 1, comm: "sh", cgroup: "/user.slice/user-1000.slice/session-1.scope"},
		{pkey: pkey{pid: 20}, ppid: 1, comm: "Xwayland", cgroup: u + "/app.slice/x.service"},
		{pkey: pkey{pid: 21}, ppid: 1, comm: "uwsm", cgroup: u + "/app.slice/y.service"},
		{pkey: pkey{pid: 22}, ppid: 1, comm: "waitpid", cgroup: u + "/background.slice/wayland-session-bindpid@1200.service"},
		{pkey: pkey{pid: 23}, ppid: 1, comm: "(sd-pam)", cgroup: u + "/init.scope"},
		{pkey: pkey{pid: 24}, ppid: 1, comm: "hyprlock", cgroup: u + "/app.slice/app-Hyprland-hyprlock-1a2b3c4d.scope"},
		{pkey: pkey{pid: 30}, ppid: 1, comm: "syncthing", cgroup: u + "/app.slice/syncthing.service"},
		{pkey: pkey{pid: 31}, ppid: 1, comm: "elephant", cgroup: u + "/background.slice/elephant.service"},
		{pkey: pkey{pid: 32}, ppid: 1, comm: "foot", cgroup: "/user.slice/user-1000.slice/session-3.scope"},
	}
	gs := map[string]group{}
	for _, g := range groupProcs(ps, map[int]string{32: "foot"}, nil) {
		gs[g.id] = g
	}
	for _, id := range []string{"comm:gvfsd", "comm:tmux: server", "comm:sh", "comm:Xwayland", "comm:uwsm", "comm:waitpid",
		"comm:(sd-pam)", "app-Hyprland-hyprlock-1a2b3c4d.scope"} {
		if !gs[id].protected {
			t.Errorf("%s is not protected", id)
		}
	}
	for _, id := range []string{"comm:syncthing", "comm:elephant", "comm:foot"} {
		if g, ok := gs[id]; !ok || g.protected {
			t.Errorf("%s = %+v (found %v), want it actionable", id, g, ok)
		}
	}
}

func TestClassName(t *testing.T) {
	for in, want := range map[string]string{
		"org.omarchy.btop":                      "btop",
		"com.mitchellh.ghostty":                 "ghostty",
		"com.spotify.Client":                    "spotify",
		"kitty":                                 "kitty",
		"chrome-www.tradingview.com__chart_X-D": "chrome-www.tradingview.com__chart_X-D",
		"Steam":                                 "Steam",
	} {
		if got := className(in); got != want {
			t.Errorf("className(%q) = %q, want %q", in, got, want)
		}
	}
}
