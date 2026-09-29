package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/view"
)

func TestLazylazySectionsUsesItsLanguage(t *testing.T) {
	t.Setenv("LAZYLAZY_LANG", "es")
	a, _, out, _ := newApp(t, "")
	if code := a.run([]string{"lazylazy", "sections"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	var secs []view.Section
	if err := json.Unmarshal(out.Bytes(), &secs); err != nil {
		t.Fatal(err)
	}
	if len(secs) == 0 || secs[0].Title != "Resumen" {
		t.Errorf("sections = %+v", secs)
	}
}

func TestLazylazyViewFirmwareIsFullscreen(t *testing.T) {
	a, _, out, _ := newApp(t, "")
	if code := a.run([]string{"lazylazy", "view", "about"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	var v view.View
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	for _, ac := range v.Actions {
		if ac.ID == "firmware.check" && ac.Mode != "fullscreen" {
			t.Errorf("firmware.check mode = %q", ac.Mode)
		}
	}
	if v.Title == "" || len(v.Actions) == 0 { // el engine falso no trae System: About no tiene bloques
		t.Errorf("view = %+v", v)
	}
}

func TestLazylazyViewReadsPanelUnits(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	os.MkdirAll(filepath.Join(dir, "sysoscope"), 0o755)
	os.WriteFile(filepath.Join(dir, "sysoscope", "config.json"), []byte(`{"units":"f","cards":["cpu"]}`), 0o644)
	a, _, out, _ := newApp(t, "")
	if code := a.run([]string{"lazylazy", "view", "summary"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), "°F") {
		t.Errorf("summary without °F: %s", out.String())
	}
}

func TestLazylazyStreamOneViewPerLine(t *testing.T) {
	a, _, out, _ := newApp(t, "") // stdin vacío: termina después del primer tick
	if code := a.run([]string{"lazylazy", "stream", "cpu"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var l struct {
		ID       string         `json:"id"`
		Blocks   []view.Block   `json:"blocks"`
		Sections []view.Section `json:"sections"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &l); err != nil {
		t.Fatal(err)
	}
	if l.ID != "cpu" || len(l.Blocks) == 0 || len(l.Sections) == 0 {
		t.Errorf("line = %+v", l)
	}
}

func TestLazylazyUnknown(t *testing.T) {
	for _, args := range [][]string{{"lazylazy"}, {"lazylazy", "nope"}, {"lazylazy", "view", "nope"}, {"lazylazy", "action", "nope"}} {
		a, _, _, _ := newApp(t, "")
		if code := a.run(args); code == 0 {
			t.Errorf("%v: exit 0", args)
		}
	}
}

func TestLazylazyFirmwareAction(t *testing.T) {
	a, _, out, _ := newApp(t, "")
	if code := a.run([]string{"lazylazy", "action", "firmware.check"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if strings.TrimSpace(out.String()) == "" {
		t.Error("no output")
	}
}

// La primera línea sale de un engine con dos lecturas, como fresh: con una
// sola, faltan los valores que se calculan por diferencia (uso de CPU).
func TestLazylazyStreamWarmsUp(t *testing.T) {
	a, fake, out, _ := newApp(t, "")
	if code := a.run([]string{"lazylazy", "stream", "cpu"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if fake.ticks < 2 || strings.Count(strings.TrimSpace(out.String()), "\n") != 0 {
		t.Errorf("ticks %d, output lines %d", fake.ticks, strings.Count(out.String(), "\n"))
	}
}
