package view

import (
	"slices"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/engine"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// focusSnap es el segundo tick de un engine con esa sección abierta.
func focusSnap(t *testing.T, fixture, focus string) *model.Snapshot {
	t.Helper()
	e := engine.New(source.Fixture(t, fixture), engine.Options{Interval: 2 * time.Second, Statfs: fakeStatfs, Focus: focus})
	e.Tick()
	return e.Tick()
}

func cellsOf(b *Block) [][]string {
	var out [][]string
	if b != nil {
		for _, r := range tableRows(*b) {
			out = append(out, r.Cells)
		}
	}
	return out
}

// La máquina de referencia: solo demoras normales, suspende a RAM y la
// despiertan el receptor, el teclado y la I211.
func TestPowerSleepReference(t *testing.T) {
	v := buildPower(t, focusSnap(t, "ultrawide", "power"), "en")
	tx := texts(v)
	if _, ok := tx["Delaying sleep briefly (normal): prog1, prog2, prog3"]; !ok {
		t.Fatalf("texts = %v", tx)
	}
	if lv, ok := tx["Nothing is keeping the computer or the screen awake"]; !ok || lv != "ok" {
		t.Fatalf("texts = %v", tx)
	}
	if table(v, "Preventing sleep or idle") != nil {
		t.Fatal("no block inhibitors here")
	}
	if got := kvValue(v.Blocks, "Sleep", "Suspend mode"); got != "Suspend to RAM (S3) (also: s2idle)" {
		t.Errorf("mode = %q", got)
	}
	want := "USB Receiver, 2.4G Wireless Keyboard, Intel Corporation I211 Gigabit Network Connection (if Wake-on-LAN is on)"
	if got := kvValue(v.Blocks, "Sleep", "Can wake the computer"); got != want {
		t.Errorf("wake = %q", got)
	}
	noEmptyValues(t, v)
}

// El laptop: un block de la tapa (tabla) y una demora; en español.
func TestPowerSleepLaptop(t *testing.T) {
	s := focusSnap(t, "laptop", "power")
	v := buildPower(t, s, "en")
	if got := cellsOf(table(v, "Preventing sleep or idle")); !slices.Equal(got[0], []string{"prog1", "lid switch", "Lid handled by the compositor"}) || len(got) != 1 {
		t.Fatalf("table = %q", got)
	}
	tx := texts(v)
	if _, ok := tx["Delaying sleep briefly (normal): prog2"]; !ok {
		t.Errorf("texts = %v", tx)
	}
	if _, ok := tx["Nothing is keeping the computer or the screen awake"]; ok {
		t.Error("a block inhibitor is keeping it awake")
	}
	if got := kvValue(v.Blocks, "Sleep", "Suspend mode"); got != "Modern standby (s2idle) (also: deep)" {
		t.Errorf("mode = %q", got)
	}
	s.Lang = "es"
	es := buildPower(t, s, "es")
	if got := cellsOf(table(es, "Impiden suspender o apagar la pantalla")); len(got) != 1 || got[0][1] != "cerrar la tapa" {
		t.Fatalf("es table = %q", got)
	}
	if got := kvValue(es.Blocks, "Suspensión", "Modo de suspensión"); got != "Espera moderna (s2idle) (también: deep)" {
		t.Errorf("es mode = %q", got)
	}
}

// Ventanas, mantener despierto, varias cosas en un inhibidor, un modo
// block-weak, qué desconocido, demoras repetidas y un modo raro.
func TestPowerSleepInvented(t *testing.T) {
	p := &model.Power{Inhibit: &model.Inhibit{StayAwake: true,
		Windows: []model.IdleWindow{{Class: "mpv", Title: "movie.mkv"}, {Class: "firefox"}},
		Logind: []model.Inhibitor{
			{What: []string{"sleep", "idle"}, Who: "Steam", Why: "Downloading", Mode: "block", UID: 1000, PID: 42},
			{What: []string{"handle-power-key", "weird-thing"}, Mode: "block-weak"},
			{What: []string{"sleep"}, Who: "UPower", Why: "x", Mode: "delay"},
			{What: []string{"sleep"}, Who: "UPower", Why: "y", Mode: "delay"},
		}},
		SleepMode: "shallow", SleepModes: []string{"shallow"}}
	v := buildPower(t, &model.Snapshot{Lang: "en", Power: p}, "en")
	tx := texts(v)
	for _, s := range []string{"Stay awake is on (Omarchy): the screen does not lock or turn off by itself",
		"Keeps the screen on: mpv — movie.mkv", "Keeps the screen on: firefox", "Delaying sleep briefly (normal): UPower"} {
		if _, ok := tx[s]; !ok {
			t.Errorf("missing %q in %v", s, tx)
		}
	}
	got := cellsOf(table(v, "Preventing sleep or idle"))
	if len(got) != 2 || !slices.Equal(got[0], []string{"Steam", "sleep, screen off when idle", "Downloading"}) ||
		!slices.Equal(got[1], []string{"—", "power key, weird-thing", "—"}) {
		t.Fatalf("table = %q", got)
	}
	if m := kvValue(v.Blocks, "Sleep", "Suspend mode"); m != "Standby (S1)" {
		t.Errorf("mode = %q", m)
	}
	if w := kvValue(v.Blocks, "Sleep", "Can wake the computer"); w != "" {
		t.Errorf("wake = %q", w)
	}
	p.SleepMode, p.SleepModes = "hibernate-ish", []string{"hibernate-ish"}
	if m := kvValue(buildPower(t, &model.Snapshot{Lang: "en", Power: p}, "en").Blocks, "Sleep", "Suspend mode"); m != "hibernate-ish" {
		t.Errorf("unknown mode = %q", m)
	}
	// Energía sin leer los inhibidores (sección cerrada): nada de esto.
	v = buildPower(t, &model.Snapshot{Lang: "en", Power: &model.Power{}}, "en")
	if table(v, "Preventing sleep or idle") != nil || len(texts(v)) != 0 {
		t.Fatalf("not read: %v", texts(v))
	}
}

// Sin logind: la razón, sin romper la sección.
func TestPowerSleepUnavailable(t *testing.T) {
	s := &model.Snapshot{Lang: "en", Power: &model.Power{Inhibit: &model.Inhibit{}},
		Unavailable: []model.Unavailable{unavailable("power.inhibitors", "not present on this system")}}
	v := buildPower(t, s, "en")
	if lv, ok := texts(v)["not available: not present on this system"]; !ok || lv != "warn" {
		t.Fatalf("texts = %v", texts(v))
	}
	if _, ok := texts(v)["Nothing is keeping the computer or the screen awake"]; ok {
		t.Fatal("ok line shown though logind could not be read")
	}
}

// Sin poder leer las ventanas de Hyprland tampoco se puede asegurar que nada
// mantenga despierta la pantalla.
func TestPowerSleepIdleWindowsUnavailable(t *testing.T) {
	s := &model.Snapshot{Lang: "en", Power: &model.Power{Inhibit: &model.Inhibit{}},
		Unavailable: []model.Unavailable{unavailable("power.idle_windows", "Hyprland did not answer")}}
	v := buildPower(t, s, "en")
	if _, ok := texts(v)["Nothing is keeping the computer or the screen awake"]; ok {
		t.Fatalf("ok line shown though idle windows could not be read: %v", texts(v))
	}
}
