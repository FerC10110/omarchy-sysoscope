package maint

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

const testDB = "/tmp/checkup-db-1000"

func repoKey(names ...string) string {
	return "cmd/" + source.Key("pacman", append([]string{"-Sddp", "--print-format", "%r %n", "--dbpath", testDB}, names...)...) + ".txt"
}

var checkKey = "cmd/" + source.Key("checkupdates", "--nocolor")

// Review Focus 2: ruido, epochs y nombres que no son paquetes.
func TestCheckUpdatesParses(t *testing.T) {
	env, dir := tempEnv(t)
	put(t, dir, checkKey+".txt", "==> Some noise from a mirror\nlinux 7.2.5-3 -> 7.2.6-1\n"+
		"mesa 1:26.2.1-1 -> 1:26.2.2-1\nwarning: something -> odd\n-x 1 -> 2\n\n  \n")
	put(t, dir, repoKey("linux", "mesa"), "core linux\nextra mesa\n")
	u := CheckUpdates(env, testDB)
	want := []model.PkgUpdate{{Name: "linux", Old: "7.2.5-3", New: "7.2.6-1", Repo: "core"},
		{Name: "mesa", Old: "1:26.2.1-1", New: "1:26.2.2-1", Repo: "extra"}}
	if u.Error != "" || !reflect.DeepEqual(u.Repo, want) || !u.Checked.Equal(source.FixtureNow) || u.Running || u.Stale {
		t.Fatalf("updates = %+v", u)
	}
	r := runner(env)
	if r.Count("yay") != 0 || r.Count("paru") != 0 || len(r.Calls) != 2 {
		t.Fatalf("calls = %v: only checkupdates and pacman -Sddp may run, never the AUR", r.Calls)
	}
	// Sin la base de checkupdates (pacman -Sddp falla): la lista sigue, sin repositorio.
	os.Remove(filepath.Join(dir, repoKey("linux", "mesa")))
	if u := CheckUpdates(env, testDB); len(u.Repo) != 2 || u.Repo[0].Repo != "" || u.Error != "" {
		t.Fatalf("without repos = %+v", u)
	}
}

func TestCheckUpdatesExitCodes(t *testing.T) {
	// 2 = no hay pendientes.
	env, dir := tempEnv(t)
	put(t, dir, checkKey+".txt", "")
	put(t, dir, checkKey+".exit", "2\n")
	if u := CheckUpdates(env, testDB); u.Error != "" || u.Repo == nil || len(u.Repo) != 0 || runner(env).Count("pacman") != 0 {
		t.Fatalf("exit 2 = %+v", u)
	}
	// Otro código: error, con el motivo.
	env, dir = tempEnv(t)
	put(t, dir, checkKey+".txt", "")
	put(t, dir, checkKey+".exit", "1\n")
	if u := CheckUpdates(env, testDB); u.Error != "exit status 1" || len(u.Repo) != 0 {
		t.Fatalf("exit 1 = %+v", u)
	}
	// Sin pacman-contrib: no corre nada de repos.
	env, _ = tempEnv(t)
	if u := CheckUpdates(env, testDB); u.Error != "requires pacman-contrib" || runner(env).Count("pacman") != 0 {
		t.Fatalf("not installed = %+v", u)
	}
	env.Lang = "es"
	if u := CheckUpdates(env, testDB); u.Error != "requiere pacman-contrib" {
		t.Fatalf("es = %q", u.Error)
	}
}

// errRunner devuelve siempre err y anota el plazo que recibió.
type errRunner struct {
	err   error
	left  time.Duration
	names []string
}

func (e *errRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	e.names = append(e.names, name)
	if dl, ok := ctx.Deadline(); ok {
		e.left = time.Until(dl)
	}
	return nil, e.err
}

func TestCheckUpdatesStderrAndTimeout(t *testing.T) {
	env, _ := tempEnv(t)
	er := &errRunner{err: &exec.ExitError{Stderr: []byte("==> ERROR: Cannot fetch updates\n\x1b[0m\n")}}
	env.Run = er
	if u := CheckUpdates(env, testDB); u.Error != "==> ERROR: Cannot fetch updates" {
		t.Fatalf("stderr = %q", u.Error)
	}
	if er.left < 50*time.Second || er.left > CheckTimeout || !reflect.DeepEqual(er.names, []string{"checkupdates"}) {
		t.Fatalf("deadline %v, calls %v", er.left, er.names)
	}
	env.Run = &errRunner{err: context.DeadlineExceeded}
	if u := CheckUpdates(env, testDB); u.Error != "timed out" {
		t.Fatalf("timeout = %q", u.Error)
	}
}

func TestCheckupDB(t *testing.T) {
	for _, c := range []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"CHECKUPDATES_DB": "/x/db/"}, "/x/db"},
		{map[string]string{"TMPDIR": "/run/tmp"}, "/run/tmp/checkup-db-1000"},
		{map[string]string{}, "/tmp/checkup-db-1000"},
	} {
		if got := CheckupDB(func(k string) string { return c.env[k] }, 1000); got != c.want {
			t.Errorf("%v = %q, want %q", c.env, got, c.want)
		}
	}
}

func TestLoadUpdatesAndStale(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, UpdatesFile)
	os.WriteFile(p, []byte(`{"checked":"2026-09-23T12:00:00Z","repo":[{"name":"linux","old":"1","new":"2","repo":"core"}],"running":true,"stale":true}`), 0o644)
	u, err := LoadUpdates(p)
	if err != nil || u.Running || u.Stale || len(u.Repo) != 1 || !u.Checked.Equal(source.FixtureNow) {
		t.Fatalf("load = %+v, %v", u, err)
	}
	for _, bad := range []string{"not json", `{"repo":null}`} {
		os.WriteFile(p, []byte(bad), 0o644)
		if _, err := LoadUpdates(p); err == nil {
			t.Errorf("%q loaded", bad)
		}
	}
	if _, err := LoadUpdates(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("a missing file loaded")
	}
	checked := &model.Updates{Checked: source.FixtureNow}
	for _, c := range []struct {
		u    *model.Updates
		up   *model.Upgrade
		want bool
	}{
		{checked, &model.Upgrade{LastChange: source.FixtureNow.Add(time.Minute)}, true},
		{checked, &model.Upgrade{LastChange: source.FixtureNow.Add(-time.Minute)}, false},
		{checked, nil, false},
		{&model.Updates{}, &model.Upgrade{LastChange: source.FixtureNow}, false},
		{nil, &model.Upgrade{LastChange: source.FixtureNow}, false},
	} {
		if got := Stale(c.u, c.up); got != c.want {
			t.Errorf("Stale(%+v, %+v) = %v", c.u, c.up, got)
		}
	}
}
