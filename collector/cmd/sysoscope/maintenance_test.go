package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/maint"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/view"
)

func TestStreamUpdatesCheckAndRefresh(t *testing.T) {
	_, fake, lines, errb := streamLines(t, "action updates.check\nrefresh\nquit\n")
	if fake.updChecks != 1 || fake.maintRefresh != 1 || strings.Contains(errb, "unknown action") || len(lines) < 1 {
		t.Fatalf("checks %d, refresh %d, stderr %q", fake.updChecks, fake.maintRefresh, errb)
	}
}

func TestLazylazyUpdatesAction(t *testing.T) {
	t.Setenv("LAZYLAZY_LANG", "en")
	t.Setenv("CHECKUPDATES_DB", "/tmp/checkup-db-test")
	a, _, out, _ := newApp(t, "")
	dir := t.TempDir()
	for name, body := range map[string]string{
		source.Key("checkupdates", "--nocolor"): "linux 7.2.5-3 -> 7.2.6-1\nmesa 1 -> 2\n",
		source.Key("pacman", "-Sddp", "--print-format", "%r %n", "--dbpath", "/tmp/checkup-db-test", "linux", "mesa"): "core linux\n",
	} {
		os.WriteFile(filepath.Join(dir, name+".txt"), []byte(body), 0o644)
	}
	fr := &source.FakeRunner{Dir: dir}
	a.env.Run = fr
	if code := a.run([]string{"lazylazy", "action", "updates.check"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	want := "linux 7.2.5-3 → 7.2.6-1 (core)\nmesa 1 → 2 (—)\n" +
		"AUR packages are not checked (that would send their names to aur.archlinux.org)\n"
	if out.String() != want {
		t.Fatalf("output = %q", out.String())
	}
	if fr.Count("yay") != 0 || fr.Count("paru") != 0 {
		t.Fatal("the AUR must never be asked")
	}
	if u, err := maint.LoadUpdates(filepath.Join(a.env.Cache, maint.UpdatesFile)); err != nil || len(u.Repo) != 2 {
		t.Fatalf("updates.json = %+v, %v", u, err)
	}
	out.Reset()
	a.env.Run = &source.FakeRunner{Dir: t.TempDir()} // sin pacman-contrib
	a.run([]string{"lazylazy", "action", "updates.check"})
	if !strings.HasPrefix(out.String(), "Could not check for updates: requires pacman-contrib\n") {
		t.Fatalf("without checkupdates = %q", out.String())
	}
	out.Reset()
	os.WriteFile(filepath.Join(dir, source.Key("checkupdates", "--nocolor")+".exit"), []byte("2\n"), 0o644)
	a.env.Run = &source.FakeRunner{Dir: dir}
	a.run([]string{"lazylazy", "action", "updates.check"})
	if !strings.HasPrefix(out.String(), "No updates available\n") {
		t.Fatalf("none = %q", out.String())
	}
}

func TestLazylazyViewMaintenanceIsFullscreen(t *testing.T) {
	t.Setenv("LAZYLAZY_LANG", "en")
	a, _, out, _ := newApp(t, "")
	if code := a.run([]string{"lazylazy", "view", "maintenance"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	var v view.View
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Actions) != 1 || v.Actions[0].ID != "updates.check" || v.Actions[0].Mode != "fullscreen" || v.Actions[0].Key != "u" {
		t.Fatalf("actions = %+v", v.Actions)
	}
}

func TestSnapshotMaintenanceSection(t *testing.T) {
	a, fake, out, _ := newApp(t, "")
	if code := a.run([]string{"snapshot", "--section", "maintenance"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if len(fake.focus) != 1 || fake.focus[0] != "maintenance" {
		t.Fatalf("focus = %q", fake.focus)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || got["schema"] == nil {
		t.Fatalf("snapshot = %s", out.String())
	}
}

func TestMaintenanceFocus(t *testing.T) {
	if engineFocus("maintenance") != "maintenance" {
		t.Fatal("engineFocus")
	}
	s := &model.Snapshot{Apps: &model.Apps{List: []model.App{{ID: "x"}}}}
	if latestServes(s, "maintenance") {
		t.Fatal("latest.json lacks the caches and snapshots")
	}
}
