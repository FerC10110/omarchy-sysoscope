package apps

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

func TestFreezeScopeWritesCgroup(t *testing.T) {
	env := tempApps(t)
	sigs := stubSignals(t, nil)
	g := groupByID(t, env, "app-org.chromium.Chromium-200.scope")
	cg := cgFreeze(t, env, g)
	// 202 y 203, hijos del navegador, quedaron en el scope que abrió Hyprland.
	other := env.FS.Real("/sys/fs/cgroup/user.slice/user-1000.slice/user@1000.service/app.slice/app-graphical.slice/app-Hyprland-chromium-db5eb250.scope/cgroup.freeze")
	f := LoadFreezer(env)
	if err := f.freeze(env, g, t0); err != nil {
		t.Fatal(err)
	}
	if readFile(t, cg) != "1" || readFile(t, other) != "1" || !f.Frozen(g.id) || len(*sigs) != 0 {
		t.Fatalf("cgroup.freeze = %q / %q, frozen %v, signals %v", readFile(t, cg), readFile(t, other), f.Frozen(g.id), *sigs)
	}
	if l := f.List(); len(l) != 1 || l[0].Name != "Chromium" || !l[0].Until.Equal(t0.Add(5*time.Minute)) {
		t.Fatalf("list = %+v", l)
	}
	if !f.Resume(env, g.id) || readFile(t, cg) != "0" || readFile(t, other) != "0" || f.Frozen(g.id) {
		t.Fatal("resume did not thaw the cgroups")
	}
	if f.Resume(env, g.id) {
		t.Fatal("resumed twice")
	}
}

func TestFreezeWithoutScopeStopsEachProcess(t *testing.T) {
	env := tempApps(t)
	sigs := stubSignals(t, nil)
	f := LoadFreezer(env)
	if err := f.freeze(env, groupByID(t, env, "comm:steam"), t0); err != nil {
		t.Fatal(err)
	}
	f.Resume(env, "comm:steam")
	if want := []string{"600 STOP", "601 STOP", "600 CONT", "601 CONT"}; !slices.Equal(*sigs, want) {
		t.Fatalf("signals = %v, want %v", *sigs, want)
	}
}

// Sin permiso sobre un proceso, nada queda a medias: lo que se detuvo se reanuda.
func TestFreezeUndoesOnFailure(t *testing.T) {
	env := tempApps(t)
	var got []string
	old := sendSignal
	sendSignal = func(pid int, sig syscall.Signal) error {
		if pid == 601 && sig == syscall.SIGSTOP {
			return syscall.EPERM
		}
		got = append(got, fmt.Sprintf("%d %s", pid, sigNames[sig]))
		return nil
	}
	t.Cleanup(func() { sendSignal = old })
	f := LoadFreezer(env)
	if err := f.freeze(env, groupByID(t, env, "comm:steam"), t0); !errors.Is(err, syscall.EPERM) {
		t.Fatalf("err = %v", err)
	}
	if !slices.Equal(got, []string{"600 STOP", "600 CONT"}) || f.Frozen("comm:steam") {
		t.Fatalf("signals %v, frozen %v", got, f.Frozen("comm:steam"))
	}
}

func TestResumeSkipsReusedPID(t *testing.T) {
	env := tempApps(t)
	sigs := stubSignals(t, nil)
	f := LoadFreezer(env)
	f.freeze(env, groupByID(t, env, "comm:steam"), t0)
	// 601 murió y otro proceso heredó el pid (otro arranque).
	os.WriteFile(env.FS.Real("/proc/601/stat"), []byte("601 (other) S 1 0 0 0 -1 0 0 0 0 0 1 1 0 0 20 0 1 0 9999 0 1\n"), 0o644)
	f.Resume(env, "comm:steam")
	if want := []string{"600 STOP", "601 STOP", "600 CONT"}; !slices.Equal(*sigs, want) {
		t.Fatalf("signals = %v, want %v", *sigs, want)
	}
}

func TestResumeDue(t *testing.T) {
	env := tempApps(t)
	stubSignals(t, nil)
	f := LoadFreezer(env)
	f.freeze(env, groupByID(t, env, "comm:steam"), t0)
	if got := f.ResumeDue(env, t0.Add(4*time.Minute)); len(got) != 0 {
		t.Fatalf("resumed early: %v", got)
	}
	got := f.ResumeDue(env, t0.Add(5*time.Minute))
	if !slices.Equal(got, []string{"steam"}) || f.Frozen("comm:steam") {
		t.Fatalf("resumed %v", got)
	}
	if n := env.Run.(*source.FakeRunner).Count("notify-send"); n != 1 {
		t.Fatalf("notify-send = %d", n)
	}
}

func TestResumeDueAfterRestart(t *testing.T) {
	env := tempApps(t)
	sigs := stubSignals(t, nil)
	LoadFreezer(env).freeze(env, groupByID(t, env, "comm:steam"), t0)
	// Otro colector (reiniciado, o el de lazylazy) lee el mismo archivo.
	g := LoadFreezer(env)
	if !g.Frozen("comm:steam") {
		t.Fatal("the new freezer does not see the frozen app")
	}
	g.ResumeDue(env, t0.Add(6*time.Minute))
	if n := len(*sigs); n != 4 || g.Frozen("comm:steam") {
		t.Fatalf("signals %v", *sigs)
	}
}

func TestResumeDueSeesAnotherWriter(t *testing.T) {
	env := tempApps(t)
	stubSignals(t, nil)
	a, b := LoadFreezer(env), LoadFreezer(env)
	a.freeze(env, groupByID(t, env, "comm:steam"), t0)
	b.ResumeDue(env, t0) // relee: todavía no vence
	if !b.Frozen("comm:steam") {
		t.Fatal("b did not reload the file written by a")
	}
}

// Un scope con un miembro en otro cgroup (un descendiente que quedó en la
// sesión): se congela el cgroup del scope, no el del primer proceso, y el
// miembro de afuera recibe SIGSTOP.
func TestFreezeScopeWithForeignMember(t *testing.T) {
	env := tempApps(t)
	sigs := stubSignals(t, nil)
	g := groupByID(t, env, "app-org.chromium.Chromium-200.scope")
	cg := cgFreeze(t, env, g)
	steam := groupByID(t, env, "comm:steam").procs[0]
	foreign := cgFreeze(t, env, group{procs: []proc{steam}})
	g.procs = append([]proc{steam}, g.procs...)
	f := LoadFreezer(env)
	if err := f.freeze(env, g, t0); err != nil {
		t.Fatal(err)
	}
	if readFile(t, cg) != "1" || readFile(t, foreign) != "0" || !slices.Equal(*sigs, []string{"600 STOP"}) {
		t.Fatalf("scope %q, foreign %q, signals %v", readFile(t, cg), readFile(t, foreign), *sigs)
	}
	if !f.Resume(env, g.id) || readFile(t, cg) != "0" || !slices.Equal(*sigs, []string{"600 STOP", "600 CONT"}) {
		t.Fatalf("after resume: scope %q, signals %v", readFile(t, cg), *sigs)
	}
}

// Si no se puede guardar, no queda nada congelado sin registrar.
func TestFreezeUndoesWhenSaveFails(t *testing.T) {
	env := tempApps(t)
	sigs := stubSignals(t, nil)
	// frozen.json es un directorio: el rename final falla.
	os.Mkdir(filepath.Join(env.State, "frozen.json"), 0o755)
	g := groupByID(t, env, "app-org.chromium.Chromium-200.scope")
	cg := cgFreeze(t, env, g)
	f := LoadFreezer(env)
	if err := f.freeze(env, g, t0); err == nil || readFile(t, cg) != "0" || f.Frozen(g.id) {
		t.Fatalf("scope: err %v, cgroup %q, frozen %v", err, readFile(t, cg), f.Frozen(g.id))
	}
	if err := f.freeze(env, groupByID(t, env, "comm:steam"), t0); err == nil || f.Frozen("comm:steam") {
		t.Fatalf("steam: err %v, frozen %v", err, f.Frozen("comm:steam"))
	}
	if want := []string{"600 STOP", "601 STOP", "600 CONT", "601 CONT"}; !slices.Equal(*sigs, want) {
		t.Fatalf("signals = %v, want %v", *sigs, want)
	}
}

func TestFreezeFromTwoFreezers(t *testing.T) {
	env := tempApps(t)
	stubSignals(t, nil)
	a, b := LoadFreezer(env), LoadFreezer(env)
	if err := a.freeze(env, groupByID(t, env, "comm:steam"), t0); err != nil {
		t.Fatal(err)
	}
	if err := b.freeze(env, groupByID(t, env, "app-org.chromium.Chromium-200.scope"), t0); err != nil {
		t.Fatal(err)
	}
	c := LoadFreezer(env)
	if !c.Frozen("comm:steam") || !c.Frozen("app-org.chromium.Chromium-200.scope") {
		t.Fatalf("list = %+v", c.List())
	}
}

// ResumeDue no pisa lo que otro agregó después de su última lectura, aunque
// el archivo conserve la misma hora de modificación (relojes gruesos).
func TestResumeDueKeepsAnotherWritersEntry(t *testing.T) {
	env := tempApps(t)
	stubSignals(t, nil)
	a, b := LoadFreezer(env), LoadFreezer(env)
	a.freeze(env, groupByID(t, env, "comm:steam"), t0)
	b.ResumeDue(env, t0) // b lee la entrada de steam
	p := filepath.Join(env.State, "frozen.json")
	st, _ := os.Stat(p)
	a.freeze(env, groupByID(t, env, "app-org.chromium.Chromium-200.scope"), t0.Add(4*time.Minute))
	os.Chtimes(p, st.ModTime(), st.ModTime())
	if got := b.ResumeDue(env, t0.Add(5*time.Minute)); !slices.Equal(got, []string{"steam"}) {
		t.Fatalf("resumed %v", got)
	}
	if c := LoadFreezer(env); !c.Frozen("app-org.chromium.Chromium-200.scope") || c.Frozen("comm:steam") {
		t.Fatalf("list = %+v", c.List())
	}
}

// Un proceso que murió (o cuyo pid heredó otro) entre la muestra y el
// congelamiento no recibe SIGSTOP.
func TestFreezeSkipsReusedPID(t *testing.T) {
	env := tempApps(t)
	sigs := stubSignals(t, nil)
	g := groupByID(t, env, "comm:steam")
	os.WriteFile(env.FS.Real("/proc/601/stat"), []byte("601 (other) S 1 0 0 0 -1 0 0 0 0 0 1 1 0 0 20 0 1 0 9999 0 1\n"), 0o644)
	f := LoadFreezer(env)
	if err := f.freeze(env, g, t0); err != nil || !f.Frozen("comm:steam") {
		t.Fatalf("err %v, frozen %v", err, f.Frozen("comm:steam"))
	}
	if !slices.Equal(*sigs, []string{"600 STOP"}) {
		t.Fatalf("signals = %v", *sigs)
	}
}

// ESRCH: el proceso terminó; se sigue con los demás.
func TestFreezeSkipsExitedProcess(t *testing.T) {
	env := tempApps(t)
	var got []string
	old := sendSignal
	sendSignal = func(pid int, sig syscall.Signal) error {
		if pid == 601 && sig == syscall.SIGSTOP {
			return syscall.ESRCH
		}
		got = append(got, fmt.Sprintf("%d %s", pid, sigNames[sig]))
		return nil
	}
	t.Cleanup(func() { sendSignal = old })
	f := LoadFreezer(env)
	if err := f.freeze(env, groupByID(t, env, "comm:steam"), t0); err != nil || !f.Frozen("comm:steam") {
		t.Fatalf("err %v, frozen %v", err, f.Frozen("comm:steam"))
	}
	f.Resume(env, "comm:steam")
	if !slices.Equal(got, []string{"600 STOP", "600 CONT"}) {
		t.Fatalf("signals = %v", got)
	}
}
