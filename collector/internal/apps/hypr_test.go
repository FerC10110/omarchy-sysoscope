package apps

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

func TestReadHypr(t *testing.T) {
	h, err := readHypr(source.Fixture(t, "apps").Hypr)
	if err != nil {
		t.Fatal(err)
	}
	// En orden estable (por stableId), no por foco: la fila no se mueve
	// bajo el cursor cuando cambia el foco.
	if ws := h.windows[200]; len(ws) != 2 || ws[0].Address != "0xa2" || ws[0].StableID != "1f" || ws[0].Fullscreen != 2 ||
		ws[1].Address != "0xa1" || !ws[1].Focused || ws[1].Monitor != "DP-1" {
		t.Fatalf("chromium windows = %+v", ws)
	}
	if w := h.windows[300][0]; w.Workspace != "special:scratch" || !w.Floating || w.Focused {
		t.Fatalf("kitty window = %+v", w)
	}
	if w := h.windows[600][0]; !w.XWayland || !w.Pinned || !w.Grouped || !w.InhibitIdle {
		t.Fatalf("steam window = %+v", w)
	}
	if _, ok := h.byAddr["0xd1"]; ok || len(h.windows[999]) != 0 {
		t.Fatal("an unmapped window was kept")
	}
	// Hyprland 0.56 dice "visible" de una ventana en un workspace especial
	// escondido: lo que cuenta es el workspace de su monitor.
	if r := h.byAddr["0xb1"]; r.pid != 300 || r.visible {
		t.Fatalf("0xb1 = %+v", r)
	}
	if !h.byAddr["0xa1"].visible || !h.byAddr["0xa2"].visible || h.byAddr["0xc1"].visible {
		t.Fatal("0xa1 and 0xa2 are on screen, 0xc1 is not")
	}
	if names := h.monitorNames(); !slices.Equal(names, []string{"DP-1", "HDMI-A-1"}) {
		t.Fatalf("monitors = %v", names)
	}
	if m, ok := h.monitor("HDMI-A-1"); !ok || m.Width != 1920 || m.Scale != 1 {
		t.Fatalf("HDMI-A-1 = %+v, %v", m, ok)
	}
}

func TestReadHyprWithoutHyprland(t *testing.T) {
	if _, err := readHypr(nil); !errors.Is(err, source.ErrNoHyprland) {
		t.Fatalf("nil: %v", err)
	}
	if _, err := readHypr(&source.FakeHypr{Dir: t.TempDir()}); !errors.Is(err, source.ErrNoHyprland) {
		t.Fatalf("no answer: %v", err)
	}
}

// editClients reemplaza old por new en el j/clients de una copia del fixture.
func editClients(t *testing.T, env source.Env, old, new string, n int) {
	t.Helper()
	p := filepath.Join(env.Hypr.(*source.FakeHypr).Dir, source.Key("j/clients")+".txt")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	j := strings.Replace(string(b), old, new, n)
	if j == string(b) {
		t.Fatalf("the fixture has no %s", old)
	}
	if err := os.WriteFile(p, []byte(j), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Sin stableId (un Hyprland viejo) el orden estable es por dirección.
func TestReadHyprOrderWithoutStableID(t *testing.T) {
	env := tempApps(t)
	editClients(t, env, `,"stableId":"20"`, "", 1)
	editClients(t, env, `,"stableId":"1f"`, "", 1)
	// 0xa2 pasa a ser la de foco más reciente, con otra clase.
	editClients(t, env, `"focusHistoryID":0,`, `"focusHistoryID":5,`, 1)
	editClients(t, env, `"class":"chromium","title":"YouTube"`, `"class":"yt","title":"YouTube"`, 1)
	h, err := readHypr(env.Hypr)
	if err != nil {
		t.Fatal(err)
	}
	if ws := h.windows[200]; len(ws) != 2 || ws[0].Address != "0xa1" || ws[1].Address != "0xa2" {
		t.Fatalf("chromium windows = %+v", ws)
	}
	// La clase de la app sigue siendo la de la ventana de foco más reciente.
	if c := h.classes()[200]; c != "yt" {
		t.Fatalf("class = %q", c)
	}
}

// editMonitors reemplaza old por new en el j/monitors de una copia del fixture.
func editMonitors(t *testing.T, env source.Env, old, new string) {
	t.Helper()
	p := filepath.Join(env.Hypr.(*source.FakeHypr).Dir, source.Key("j/monitors")+".txt")
	b, _ := os.ReadFile(p)
	j := strings.Replace(string(b), old, new, 1)
	if j == string(b) {
		t.Fatalf("the fixture has no %s", old)
	}
	os.WriteFile(p, []byte(j), 0o644)
}

// En pantalla: el workspace activo de su monitor o su workspace especial
// abierto, sin hidden y mapeada.
func TestReadHyprOnScreen(t *testing.T) {
	env := tempApps(t)
	editMonitors(t, env, `"specialWorkspace":{"id":0,"name":""}`, `"specialWorkspace":{"id":-98,"name":"special:scratch"}`)
	editClients(t, env, `{"address":"0xa2","mapped":true,"hidden":false`, `{"address":"0xa2","mapped":true,"hidden":true`, 1)
	h, err := readHypr(env.Hypr)
	if err != nil {
		t.Fatal(err)
	}
	if !h.byAddr["0xb1"].visible {
		t.Fatal("0xb1 is on the special workspace that is open")
	}
	// El especial se abre encima: el workspace activo sigue en pantalla.
	if !h.byAddr["0xa1"].visible {
		t.Fatal("0xa1 is on the active workspace, under the special one")
	}
	if h.byAddr["0xa2"].visible {
		t.Fatal("0xa2 is hidden (a tab of a group that is not shown)")
	}
}
