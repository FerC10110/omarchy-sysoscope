package view

import (
	"slices"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

// powerSnap es una Energía inventada: balanced de tres perfiles, performance
// guardado para enchufado, un mouse y, con battery, BAT0.
func powerSnap(onAC *bool, battery, script bool) *model.Snapshot {
	p := &model.Power{Profile: "balanced", Profiles: []string{"power-saver", "balanced", "performance"}, OnAC: onAC,
		SavedAC: "performance", SetScript: script,
		Batteries: []model.Battery{{Name: "hidpp_battery_0", Model: "MX Master 3", Peripheral: true, Pct: 55, Status: "Discharging"}}}
	if battery {
		p.Batteries = append(p.Batteries, model.Battery{Name: "BAT0", Pct: 76, Status: "Discharging"})
	}
	return &model.Snapshot{Lang: "en", Power: p}
}

func buildPower(t *testing.T, s *model.Snapshot, lang string) *View {
	t.Helper()
	v, err := Build("power", s, Options{Lang: lang})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func findAction(v *View, id string) *Action {
	for i := range v.Actions {
		if v.Actions[i].ID == id {
			return &v.Actions[i]
		}
	}
	return nil
}

func TestPowerSavedRow(t *testing.T) {
	no := false
	v := buildPower(t, powerSnap(&no, true, true), "en")
	if got := kvValue(v.Blocks, "*", "Saved profiles"); got != "Plugged in: performance · Battery: —" {
		t.Errorf("en row = %q", got)
	}
	es := buildPower(t, powerSnap(&no, true, true), "es")
	if got := kvValue(es.Blocks, "*", "Perfiles guardados"); got != "Enchufado: performance · Batería: —" {
		t.Errorf("es row = %q", got)
	}
	// Un mouse no es la batería de la máquina.
	if got := kvValue(buildPower(t, powerSnap(nil, false, true), "en").Blocks, "*", "Saved profiles"); got != "" {
		t.Errorf("a desktop has no saved-profiles row: %q", got)
	}
	s := powerSnap(&no, true, true)
	s.Power.Profiles, s.Power.Profile = nil, ""
	if got := kvValue(buildPower(t, s, "en").Blocks, "*", "Saved profiles"); got != "" {
		t.Errorf("without power-profiles-daemon: %q", got)
	}
	noEmptyValues(t, v)
}

func TestPowerActions(t *testing.T) {
	yes, no := true, false
	// Sin power-profiles-daemon: ninguna.
	s := powerSnap(&no, true, true)
	s.Power.Profiles, s.Power.Profile = nil, ""
	if v := buildPower(t, s, "en"); len(v.Actions) != 0 {
		t.Fatalf("without profiles: %+v", v.Actions)
	}
	// Escritorio (sin fuente ni batería): solo p, con el activo por defecto.
	v := buildPower(t, powerSnap(nil, false, true), "en")
	if len(v.Actions) != 1 {
		t.Fatalf("desktop actions = %+v", v.Actions)
	}
	a := v.Actions[0]
	if a.Key != "p" || a.ID != "power.profile" || a.Label != "Power profile" || a.Target != "section" || a.Mode != "background" ||
		a.Confirm != "" || a.Input == nil || a.Input.Type != "choice" || a.Input.Prompt != "Profile" || a.Input.Default != "balanced" ||
		!slices.Equal(a.Input.Options, []string{"power-saver", "balanced", "performance"}) {
		t.Fatalf("p = %+v %+v", a, a.Input)
	}
	// Las opciones son una copia.
	s = powerSnap(nil, false, true)
	buildPower(t, s, "en").Actions[0].Input.Options[0] = "x"
	if s.Power.Profiles[0] != "power-saver" {
		t.Fatal("the options alias the snapshot's profiles")
	}
	// Un activo fuera de la lista: el primero.
	s = powerSnap(nil, false, true)
	s.Power.Profile = "custom"
	if d := buildPower(t, s, "en").Actions[0].Input.Default; d != "power-saver" {
		t.Fatalf("default = %q", d)
	}
	// Desenchufada: P guarda el de enchufado, con el guardado por defecto.
	v = buildPower(t, powerSnap(&no, true, true), "en")
	o := findAction(v, "power.profile_other")
	if o == nil || o.Key != "P" || o.Label != "Profile when plugged in" || o.Target != "section" || o.Mode != "background" ||
		o.Input == nil || o.Input.Type != "choice" || o.Input.Prompt != "Profile when plugged in" || o.Input.Default != "performance" {
		t.Fatalf("P = %+v", o)
	}
	// Enchufada, con un guardado que ya no existe: P es el de batería y
	// arranca en el activo.
	s = powerSnap(&yes, true, true)
	s.Power.SavedBattery = "turbo"
	o = findAction(buildPower(t, s, "en"), "power.profile_other")
	if o == nil || o.Label != "Profile on battery" || o.Input.Default != "balanced" {
		t.Fatalf("P on AC = %+v", o)
	}
	// Sin omarchy-powerprofiles-set: solo p.
	v = buildPower(t, powerSnap(&no, true, false), "en")
	if findAction(v, "power.profile_other") != nil || findAction(v, "power.profile") == nil {
		t.Fatalf("without the script: %+v", v.Actions)
	}
	// En español.
	v = buildPower(t, powerSnap(&no, true, true), "es")
	if v.Actions[0].Label != "Perfil de energía" || findAction(v, "power.profile_other").Label != "Perfil enchufado" {
		t.Fatalf("es = %+v", v.Actions)
	}
	s = powerSnap(&yes, true, true)
	if l := findAction(buildPower(t, s, "es"), "power.profile_other").Label; l != "Perfil con batería" {
		t.Fatalf("es on AC = %q", l)
	}
}
