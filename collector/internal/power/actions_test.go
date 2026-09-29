package power

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// fakeCmd escribe la salida falsa de un comando (y su código si no es 0).
func fakeCmd(t *testing.T, env source.Env, key, text string, exit int) {
	t.Helper()
	dir := env.Run.(*source.FakeRunner).Dir
	if err := os.WriteFile(filepath.Join(dir, key+".txt"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if exit != 0 {
		os.WriteFile(filepath.Join(dir, key+".exit"), []byte(strconv.Itoa(exit)), 0o644)
	}
}

func calls(env source.Env) []string { return env.Run.(*source.FakeRunner).Calls }

func actionsLog(t *testing.T, env source.Env) string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(env.State, "actions.log"))
	return string(b)
}

func TestKnown(t *testing.T) {
	if !Known("power.profile") || !Known("power.profile_other") || Known("power.nope") || Known("wifi.band") {
		t.Fatal("Known")
	}
}

// Desenchufada: el script con la fuente battery, y queda en actions.log.
func TestProfileRunsTheScript(t *testing.T) {
	env := powerEnv(t, true)
	fakeCmd(t, env, "omarchy-powerprofiles-set_battery_performance", "", 0)
	out, err := Run(env, "power.profile", "", " performance ")
	if err != nil || out != "Power profile set to performance (remembered on battery)" {
		t.Fatalf("out %q err %v", out, err)
	}
	if !slices.Equal(calls(env), []string{"omarchy-powerprofiles-set_battery_performance"}) {
		t.Fatalf("calls = %q", calls(env))
	}
	if l := actionsLog(t, env); !strings.Contains(l, `power.profile "" " performance " ok: Power profile set to performance (remembered on battery)`) {
		t.Fatalf("actions.log = %q", l)
	}
}

func TestProfileOnAC(t *testing.T) {
	env := powerEnv(t, true)
	put(t, env.FS.Root, "sys/class/power_supply/AC/online", "1\n")
	fakeCmd(t, env, "omarchy-powerprofiles-set_ac_power-saver", "", 0)
	out, err := Run(env, "power.profile", "", "power-saver")
	if err != nil || out != "Power profile set to power-saver (remembered while plugged in)" {
		t.Fatalf("out %q err %v", out, err)
	}
}

// Sin el script de Omarchy: powerprofilesctl, y se dice que no se recuerda.
func TestProfileFallback(t *testing.T) {
	env := powerEnv(t, false)
	fakeCmd(t, env, "powerprofilesctl_set_performance", "", 0)
	out, err := Run(env, "power.profile", "", "performance")
	if err != nil || out != "Power profile set to performance (not remembered: omarchy-powerprofiles-set is missing)" {
		t.Fatalf("out %q err %v", out, err)
	}
	if !slices.Equal(calls(env), []string{"powerprofilesctl_set_performance"}) {
		t.Fatalf("calls = %q", calls(env))
	}
}

func TestProfileInvalid(t *testing.T) {
	env := powerEnv(t, true)
	for _, id := range []string{"power.profile", "power.profile_other"} {
		for _, in := range []string{"turbo", "", "Balanced"} {
			_, err := Run(env, id, "", in)
			if err == nil || !strings.HasSuffix(err.Error(), "one of: power-saver, balanced, performance") {
				t.Errorf("%s %q: err %v", id, in, err)
			}
		}
	}
	if len(calls(env)) != 0 {
		t.Fatalf("nothing may run: %q", calls(env))
	}
	if _, err := os.Stat(filepath.Join(env.FS.Root, stateRel)); !os.IsNotExist(err) {
		t.Fatal("nothing may be written")
	}
	if _, err := Run(env, "power.nope", "", "balanced"); err == nil || err.Error() != `unknown action "power.nope"` {
		t.Fatalf("unknown id: %v", err)
	}
}

func TestProfileWithoutPPD(t *testing.T) {
	env := powerEnv(t, true)
	env.Bus = &source.FakeBus{}
	if _, err := Run(env, "power.profile", "", "balanced"); err == nil || err.Error() != "power-profiles-daemon is not available" {
		t.Fatalf("err %v", err)
	}
	if len(calls(env)) != 0 {
		t.Fatalf("calls = %q", calls(env))
	}
}

// deadlineRunner anota cuánto le queda al plazo y responde que venció.
type deadlineRunner struct{ left time.Duration }

func (r *deadlineRunner) Run(ctx context.Context, _ string, _ ...string) ([]byte, error) {
	d, _ := ctx.Deadline()
	r.left = time.Until(d)
	return nil, context.DeadlineExceeded
}

func TestProfileTimeout(t *testing.T) {
	env := powerEnv(t, true)
	r := &deadlineRunner{}
	env.Run = r
	if _, err := Run(env, "power.profile", "", "performance"); err == nil || err.Error() != "timed out" {
		t.Fatalf("err %v", err)
	}
	if r.left <= 3*time.Second || r.left > 4*time.Second {
		t.Fatalf("deadline in %v, want 4 s", r.left)
	}
}

func TestProfileScriptError(t *testing.T) {
	env := powerEnv(t, true)
	fakeCmd(t, env, "omarchy-powerprofiles-set_battery_performance", "\nPower profile is not available: performance\n", 1)
	if _, err := Run(env, "power.profile", "", "performance"); err == nil || err.Error() != "Power profile is not available: performance" {
		t.Fatalf("err %v", err)
	}
	if l := actionsLog(t, env); !strings.Contains(l, "error: Power profile is not available: performance") {
		t.Fatalf("actions.log = %q", l)
	}
}

// Desenchufada, P guarda el de enchufado escribiendo el archivo de Omarchy,
// sin correr nada y sin dejar temporales.
func TestProfileOther(t *testing.T) {
	env := powerEnv(t, true)
	out, err := Run(env, "power.profile_other", "", "performance")
	if err != nil || out != "Saved performance for when it is plugged in" {
		t.Fatalf("out %q err %v", out, err)
	}
	dir := filepath.Join(env.FS.Root, stateRel)
	b, err := os.ReadFile(filepath.Join(dir, "ac"))
	if err != nil || string(b) != "performance\n" {
		t.Fatalf("ac = %q, %v", b, err)
	}
	if fi, _ := os.Stat(filepath.Join(dir, "ac")); fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode %v", fi.Mode())
	}
	if es, _ := os.ReadDir(dir); len(es) != 1 {
		t.Fatalf("leftovers in %s: %v", dir, es)
	}
	if len(calls(env)) != 0 {
		t.Fatalf("calls = %q", calls(env))
	}
	// Enchufada: guarda el de batería, y reemplaza lo que había.
	put(t, env.FS.Root, "sys/class/power_supply/AC/online", "1\n")
	put(t, env.FS.Root, stateRel+"/battery", "balanced\n")
	if out, err := Run(env, "power.profile_other", "", "power-saver"); err != nil || out != "Saved power-saver for when it runs on battery" {
		t.Fatalf("out %q err %v", out, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "battery")); string(b) != "power-saver\n" {
		t.Fatalf("battery = %q", b)
	}
}

func TestProfileOtherStateDirEnv(t *testing.T) {
	env := powerEnv(t, true)
	stubEnv(t, map[string]string{"PATH": "/usr/bin", "OMARCHY_POWERPROFILES_STATE_DIR": "/pp"})
	if _, err := Run(env, "power.profile_other", "", "balanced"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(env.FS.Root, "pp", "ac")); string(b) != "balanced\n" {
		t.Fatalf("pp/ac = %q", b)
	}
}

func TestProfileOtherRefuses(t *testing.T) {
	env := powerEnv(t, false)
	if _, err := Run(env, "power.profile_other", "", "balanced"); err == nil || err.Error() != "requires omarchy-powerprofiles-set" {
		t.Fatalf("without the script: %v", err)
	}
	env = powerEnv(t, true)
	if err := os.RemoveAll(filepath.Join(env.FS.Root, "sys/class/power_supply/BAT0")); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(env, "power.profile_other", "", "balanced"); err == nil || err.Error() != "there is no battery" {
		t.Fatalf("without a battery: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.FS.Root, stateRel)); !os.IsNotExist(err) {
		t.Fatal("nothing may be written")
	}
}

func TestPowerActionsSpanish(t *testing.T) {
	env := powerEnv(t, true)
	env.Lang = "es"
	fakeCmd(t, env, "omarchy-powerprofiles-set_battery_performance", "", 0)
	if out, _ := Run(env, "power.profile", "", "performance"); out != "Perfil de energía: performance (se recuerda con batería)" {
		t.Fatalf("out %q", out)
	}
	if out, _ := Run(env, "power.profile_other", "", "balanced"); out != "Enchufado usará balanced" {
		t.Fatalf("out %q", out)
	}
	if _, err := Run(env, "power.profile", "", "x"); err == nil || err.Error() != `perfil de energía desconocido "x"; uno de: power-saver, balanced, performance` {
		t.Fatalf("err %v", err)
	}
}
