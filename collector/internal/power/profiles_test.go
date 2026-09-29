package power

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// stateRel es el directorio de estado de Omarchy dentro de la copia del
// fixture (Home = /home/user, sin XDG_STATE_HOME).
const stateRel = "home/user/.local/state/omarchy/powerprofiles"

func ppdKey(prop string) string { return ppd + " " + ppdPath + " " + ppd + " " + prop }

// stubEnv reemplaza getenv: lo que no está en vars es "".
func stubEnv(t *testing.T, vars map[string]string) {
	t.Helper()
	old := getenv
	getenv = func(k string) string { return vars[k] }
	t.Cleanup(func() { getenv = old })
}

// put escribe rel dentro de root, creando los directorios.
func put(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// powerEnv es una copia del fixture laptop (BAT0 del sistema, un mouse, sin
// corriente) con power-profiles-daemon en el bus falso (balanced de
// power-saver, balanced y performance), un directorio de comandos vacío y
// PATH=/usr/bin (de la copia). script dice si está omarchy-powerprofiles-set.
// Nada sale de la copia: el estado de Omarchy queda en <copia>/stateRel.
func powerEnv(t *testing.T, script bool) source.Env {
	t.Helper()
	env := source.Fixture(t, "laptop")
	root := copyRoot(t, &env)
	env.Run = &source.FakeRunner{Dir: t.TempDir()}
	env.Bus = &source.FakeBus{System: map[string]json.RawMessage{
		ppdKey("ActiveProfile"): json.RawMessage(`"balanced"`),
		ppdKey("Profiles"):      json.RawMessage(`[{"Profile":"power-saver"},{"Profile":"balanced"},{"Profile":"performance"}]`),
	}}
	stubEnv(t, map[string]string{"PATH": "/usr/bin"})
	if script {
		put(t, root, "usr/bin/omarchy-powerprofiles-set", "#!/bin/sh\n")
	}
	return env
}

func TestStateDir(t *testing.T) {
	env := source.Env{Home: "/home/ana"}
	for _, c := range []struct {
		vars map[string]string
		want string
	}{
		{map[string]string{}, "/home/ana/.local/state/omarchy/powerprofiles"},
		{map[string]string{"XDG_STATE_HOME": "/xdg"}, "/xdg/omarchy/powerprofiles"},
		{map[string]string{"XDG_STATE_HOME": "/xdg", "OMARCHY_POWERPROFILES_STATE_DIR": "/pp"}, "/pp"},
	} {
		stubEnv(t, c.vars)
		if got := StateDir(env); got != c.want {
			t.Errorf("StateDir with %v = %q, want %q", c.vars, got, c.want)
		}
	}
}

func TestSourceAndOther(t *testing.T) {
	yes, no := true, false
	for _, c := range []struct {
		on   *bool
		want string
	}{{nil, "ac"}, {&yes, "ac"}, {&no, "battery"}} {
		if got := Source(&model.Power{OnAC: c.on}); got != c.want {
			t.Errorf("Source(%v) = %q, want %q", c.on, got, c.want)
		}
	}
	if Other("ac") != "battery" || Other("battery") != "ac" {
		t.Error("Other")
	}
	if HasBattery(&model.Power{Batteries: []model.Battery{{Name: "hidpp_battery_0", Peripheral: true}}}) {
		t.Error("a mouse is not the machine's battery")
	}
	if !HasBattery(&model.Power{Batteries: []model.Battery{{Name: "BAT0"}}}) || HasBattery(&model.Power{}) {
		t.Error("HasBattery")
	}
}

func TestSavedProfiles(t *testing.T) {
	env := powerEnv(t, true)
	r := func() *collect.Report { return &collect.Report{Lang: "en"} }
	var s Sampler
	p := &model.Power{}
	s.Sample(env, p, true, r())
	if p.Profile != "balanced" || len(p.Profiles) != 3 || !p.SetScript || p.SavedAC != "" || p.SavedBattery != "" {
		t.Fatalf("without saved files: %+v", p)
	}
	put(t, env.FS.Root, stateRel+"/ac", "performance\ngarbage\n")
	put(t, env.FS.Root, stateRel+"/battery", "  \n")
	s.Sample(env, p, false, r())
	if p.SavedAC != "" {
		t.Fatal("a fast sample must not read the saved profiles")
	}
	s.Sample(env, p, true, r())
	if p.SavedAC != "performance" || p.SavedBattery != "" {
		t.Fatalf("saved = %q / %q", p.SavedAC, p.SavedBattery)
	}
	stubEnv(t, map[string]string{"PATH": "/usr/local/bin:/nowhere"})
	s.Sample(env, p, true, r())
	if p.SetScript {
		t.Fatal("omarchy-powerprofiles-set is not in PATH")
	}
	env.Bus = &source.FakeBus{}
	s.Sample(env, p, true, r())
	if p.Profile != "" || p.Profiles != nil || p.SavedAC != "" || p.SetScript {
		t.Fatalf("without power-profiles-daemon: %+v", p)
	}
}
