package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/engine"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/history"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/view"
)

type fakeEngine struct {
	ticks                   int
	focus                   []string
	interval                time.Duration
	refresh                 int
	closed                  bool
	lang                    string
	fwChecks                int
	wifiRefresh             int
	powerRefresh            int
	audioRefresh            int
	audioResetAvail         []bool
	btRefresh               int
	updChecks, maintRefresh int
}

func (f *fakeEngine) Tick() *model.Snapshot {
	f.ticks++
	u, temp := 12.0, 50.0
	return &model.Snapshot{Schema: 1, Lang: f.lang, Time: source.FixtureNow, CPU: &model.CPU{Model: "Fake", UsagePct: &u, TempC: &temp, Load: [3]float64{1.5, 1, 0.5}},
		Memory:      &model.Memory{TotalB: 1},
		Unavailable: []model.Unavailable{{What: "cpu.temp", Reason: "not_present", Message: "x"}, {What: "memory.modules", Reason: "missing_tool", Message: "y"}}}
}
func (f *fakeEngine) SetFocus(s string)           { f.focus = append(f.focus, s) }
func (f *fakeEngine) SetInterval(d time.Duration) { f.interval = d }
func (f *fakeEngine) Interval() time.Duration     { return f.interval }
func (f *fakeEngine) RefreshSlow()                { f.refresh++ }
func (f *fakeEngine) Close() error                { f.closed = true; return nil }
func (f *fakeEngine) History() *history.Store     { return history.New() }
func (f *fakeEngine) Recent(string) []float64     { return nil }
func (f *fakeEngine) CheckFirmware()              { f.fwChecks++ }
func (f *fakeEngine) FirmwareRunning() bool       { return false }
func (f *fakeEngine) SetLang(l string)            { f.lang = l }
func (f *fakeEngine) RefreshWifi()                { f.wifiRefresh++ }
func (f *fakeEngine) RefreshPower()               { f.powerRefresh++ }
func (f *fakeEngine) RefreshAudio(resetAvail bool) {
	f.audioRefresh++
	f.audioResetAvail = append(f.audioResetAvail, resetAvail)
}
func (f *fakeEngine) RefreshBluetooth()   { f.btRefresh++ }
func (f *fakeEngine) CheckUpdates()       { f.updChecks++ }
func (f *fakeEngine) RefreshMaintenance() { f.maintRefresh++ }

func newApp(t *testing.T, stdin string) (*app, *fakeEngine, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	fake := &fakeEngine{}
	var out, errb bytes.Buffer
	a := &app{
		env:   source.Fixture(t, "minimal"),
		stdin: strings.NewReader(stdin), stdout: &out, stderr: &errb,
		newEngine: func(env source.Env, o engine.Options) sampler {
			fake.interval, fake.lang = o.Interval, env.Lang
			if o.Focus != "" {
				fake.focus = append(fake.focus, o.Focus)
			}
			return fake
		},
		sleep: func(time.Duration) {},
	}
	return a, fake, &out, &errb
}

func TestStreamCommands(t *testing.T) {
	a, fake, out, _ := newApp(t, "focus gpu\ninterval 5\nrefresh\npause\nresume\nquit\n")
	if code := a.run([]string{"stream", "--interval", "1h"}); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if len(fake.focus) != 1 || fake.focus[0] != "gpu" || fake.interval != 5*time.Second || fake.refresh != 1 || fake.wifiRefresh != 1 || !fake.closed {
		t.Fatalf("engine = %+v", fake)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != fake.ticks || fake.ticks < 2 {
		t.Fatalf("%d lines for %d ticks", len(lines), fake.ticks)
	}
	var s model.Snapshot
	if err := json.Unmarshal([]byte(lines[0]), &s); err != nil || s.CPU.Model != "Fake" {
		t.Fatalf("ndjson line = %q (%v)", lines[0], err)
	}
	if _, err := os.Stat(filepath.Join(a.env.State, "latest.json")); err != nil {
		t.Fatal("latest.json not written")
	}
}

func TestStreamEOFAndBadCommand(t *testing.T) {
	a, fake, _, errb := newApp(t, "bogus\ninterval x\n")
	if code := a.run([]string{"stream"}); code != 0 || !fake.closed {
		t.Fatalf("exit = %d closed = %v", code, fake.closed)
	}
	if !strings.Contains(errb.String(), `unknown stream command "bogus"`) || !strings.Contains(errb.String(), "interval") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

func TestSnapshotUsesLatest(t *testing.T) {
	a, fake, out, _ := newApp(t, "")
	data, _ := json.Marshal(model.Snapshot{Schema: 1, Lang: "en", Time: source.FixtureNow, CPU: &model.CPU{Model: "FromLatest"}})
	p := filepath.Join(a.env.State, "latest.json")
	os.WriteFile(p, data, 0o644)
	now := time.Now()
	os.Chtimes(p, now, now)
	a.env.Now = func() time.Time { return now.Add(2 * time.Second) }
	if code := a.run([]string{"snapshot", "--section", "cpu"}); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if fake.ticks != 0 || !strings.Contains(out.String(), "FromLatest") {
		t.Fatalf("ticks = %d, out = %s", fake.ticks, out.String())
	}
	a.env.Now = func() time.Time { return now.Add(10 * time.Second) }
	out.Reset()
	a.run([]string{"snapshot", "--section", "cpu"})
	if fake.ticks != 2 || strings.Contains(out.String(), "FromLatest") {
		t.Fatalf("stale latest must be ignored: ticks %d", fake.ticks)
	}
}

func TestSnapshotSection(t *testing.T) {
	a, fake, out, _ := newApp(t, "")
	if code := a.run([]string{"snapshot", "--section", "cpu"}); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["cpu"]; !ok || m["memory"] != nil || string(m["schema"]) != "1" {
		t.Fatalf("section output = %s", out.String())
	}
	if !strings.Contains(string(m["unavailable"]), "cpu.temp") || strings.Contains(string(m["unavailable"]), "memory.modules") {
		t.Fatalf("unavailable not filtered: %s", m["unavailable"])
	}
	if fake.ticks != 2 {
		t.Fatalf("ticks = %d", fake.ticks)
	}
	if code := a.run([]string{"snapshot", "--section", "nope"}); code != 2 {
		t.Fatalf("unknown section exit = %d", code)
	}
}

func TestHistoryCmd(t *testing.T) {
	a, _, out, errb := newApp(t, "")
	s := history.New()
	s.Add(a.env.Now(), map[string]float64{"cpu.usage": 42})
	s.Save(filepath.Join(a.env.State, "history.v1"))
	if code := a.run([]string{"history", "--metric", "cpu.usage", "--span", "24h", "--format", "csv"}); code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if lines[0] != "time,cpu.usage" || len(lines) != 1441 || !strings.HasSuffix(lines[len(lines)-1], ",42") {
		t.Fatalf("csv = %d lines, last %q", len(lines), lines[len(lines)-1])
	}
	if code := a.run([]string{"history", "--metric", "nope"}); code != 2 {
		t.Fatalf("bad metric exit = %d", code)
	}
}

func TestVersion(t *testing.T) {
	a, _, out, _ := newApp(t, "")
	if a.run([]string{"version"}) != 0 || strings.TrimSpace(out.String()) != version {
		t.Fatalf("version = %q", out.String())
	}
	if a.run([]string{"frobnicate"}) != 2 {
		t.Fatal("unknown command must exit 2")
	}
}

func TestMaxProcs(t *testing.T) {
	if n := maxProcs(""); n != 2 {
		t.Fatalf("default = %d, want 2 (a monitor does not need every core)", n)
	}
	if n := maxProcs("8"); n != 0 {
		t.Fatalf("GOMAXPROCS set by the user must be respected, got %d", n)
	}
}

func TestGCPercent(t *testing.T) {
	if n := gcPercent(""); n != 50 {
		t.Fatalf("default = %d, want 50 (the heap is tiny; a tighter GC keeps RSS under 15 MB)", n)
	}
	if n := gcPercent("200"); n != 0 {
		t.Fatalf("GOGC set by the user must be respected, got %d", n)
	}
}

func TestSnapshotAcceptsJSONFlag(t *testing.T) {
	a, _, out, _ := newApp(t, "")
	if code := a.run([]string{"snapshot", "--json", "--section", "cpu"}); code != 0 || !json.Valid(out.Bytes()) {
		t.Fatalf("snapshot --json (spec §2.2) exit = %d, out = %s", code, out.String())
	}
}

func TestSnapshotIgnoresLatestInOtherLanguage(t *testing.T) {
	a, fake, out, _ := newApp(t, "")
	data, _ := json.Marshal(model.Snapshot{Schema: 1, Lang: "en", Time: source.FixtureNow, CPU: &model.CPU{Model: "FromLatest"}})
	p := filepath.Join(a.env.State, "latest.json")
	os.WriteFile(p, data, 0o644)
	now := time.Now()
	os.Chtimes(p, now, now)
	a.env.Now = func() time.Time { return now.Add(time.Second) }
	a.run([]string{"snapshot", "--section", "cpu", "--lang", "es"})
	if fake.ticks != 2 || strings.Contains(out.String(), "FromLatest") {
		t.Fatalf("an English latest.json must not answer a Spanish snapshot: ticks %d", fake.ticks)
	}
}

func TestSectionsCmd(t *testing.T) {
	a, _, out, _ := newApp(t, "")
	if code := a.run([]string{"sections", "--lang", "es"}); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	var secs []view.Section
	if err := json.Unmarshal(out.Bytes(), &secs); err != nil || len(secs) != 13 {
		t.Fatalf("sections = %s (%v)", out.String(), err)
	}
	for _, s := range secs {
		if s.ID == "storage" && s.Title != "Discos" {
			t.Errorf("storage title = %q", s.Title)
		}
	}
}

func TestViewCmd(t *testing.T) {
	a, fake, out, errb := newApp(t, "")
	if code := a.run([]string{"view", "cpu", "--units", "f"}); code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), `"id":"cpu"`) || !strings.Contains(out.String(), "°F") {
		t.Fatalf("view = %s", out.String())
	}
	if len(fake.focus) == 0 || fake.focus[0] != "cpu" {
		t.Errorf("engine focus = %v, want cpu", fake.focus)
	}
	if code := a.run([]string{"view", "nope"}); code != 2 || !strings.Contains(errb.String(), `unknown section "nope"`) {
		t.Fatalf("unknown view exit = %d, stderr %q", code, errb.String())
	}
}

// firstLine avisa por ready cuando el stream escribió su primera línea.
type firstLine struct {
	*bytes.Buffer
	once  sync.Once
	ready chan struct{}
}

func (f *firstLine) Write(p []byte) (int, error) {
	n, err := f.Buffer.Write(p)
	f.once.Do(func() { close(f.ready) })
	return n, err
}

// streamLines corre un stream y le manda stdin recién después de su primera
// línea: si un comando llega antes que el primer tick del timer, select puede
// elegirlo primero, su Reset descarta ese tick y sale una línea menos.
func streamLines(t *testing.T, stdin string, args ...string) (*app, *fakeEngine, []string, string) {
	t.Helper()
	a, fake, out, errb := newApp(t, "")
	inR, inW := io.Pipe()
	w := &firstLine{Buffer: out, ready: make(chan struct{})}
	a.stdin, a.stdout = inR, w
	go func() {
		<-w.ready
		io.WriteString(inW, stdin)
		inW.Close()
	}()
	t.Cleanup(func() { inW.Close() })
	if code := a.run(append([]string{"stream", "--interval", "1h"}, args...)); code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	return a, fake, strings.Split(strings.TrimSpace(out.String()), "\n"), errb.String()
}

// Memoria y Red terminan con las apps que más usan: el engine tiene que
// saber que están abiertas para leerlas.
func TestStreamFocusShowsApps(t *testing.T) {
	a, fake, _, _ := newApp(t, "focus memory\nfocus network\nfocus about\nquit\n")
	a.run([]string{"stream"})
	if want := []string{"memory", "network", ""}; strings.Join(fake.focus, ",") != strings.Join(want, ",") {
		t.Fatalf("engine focus = %q, want %q", fake.focus, want)
	}
}

func TestStreamFocusAddsView(t *testing.T) {
	a, fake, lines, _ := streamLines(t, "focus cpu\nfocus about\nquit\n")
	if len(lines) < 3 {
		t.Fatalf("lines = %d", len(lines))
	}
	for i, l := range lines {
		if !json.Valid([]byte(l)) || !strings.Contains(l, `"sections":[`) {
			t.Fatalf("line %d = %s", i, l)
		}
	}
	if strings.Contains(lines[0], `"view"`) || !strings.Contains(lines[1], `"view":{"schema":1,"id":"cpu"`) ||
		!strings.Contains(lines[2], `"id":"about"`) {
		t.Fatalf("views: %s / %s", lines[0], lines[1])
	}
	if len(fake.focus) != 2 || fake.focus[0] != "cpu" || fake.focus[1] != "" {
		t.Errorf("engine focus = %q, want [cpu \"\"]", fake.focus)
	}
	latest, _ := os.ReadFile(filepath.Join(a.env.State, "latest.json"))
	if strings.Contains(string(latest), `"view"`) || strings.Contains(string(latest), `"sections"`) {
		t.Errorf("latest.json = %s", latest)
	}
}

func TestStreamLangUnitsCards(t *testing.T) {
	_, _, lines, _ := streamLines(t, "focus cpu\nlang es\nunits f\nfocus summary\ncards load\nquit\n")
	if !strings.Contains(lines[2], `"lang":"es"`) || !strings.Contains(lines[2], `"title":"Procesador"`) {
		t.Errorf("after lang es: %s", lines[2])
	}
	if !strings.Contains(lines[3], "°F") {
		t.Errorf("after units f: %s", lines[3])
	}
	var got struct{ View view.View }
	json.Unmarshal([]byte(lines[5]), &got)
	first := ""
	for _, b := range got.View.Blocks {
		if b.Type == "cards" && len(b.Blocks) > 0 {
			first = b.Blocks[0].Label
		}
	}
	if first != "Carga" {
		t.Errorf("after cards load: %+v", got.View.Blocks)
	}
}

func TestStreamAction(t *testing.T) {
	_, fake, lines, errb := streamLines(t, "action nope\naction firmware.check\nquit\n")
	if fake.fwChecks != 1 || !strings.Contains(errb, `unknown action "nope"`) || len(lines) < 1 {
		t.Fatalf("fwChecks = %d, stderr %q", fake.fwChecks, errb)
	}
}
