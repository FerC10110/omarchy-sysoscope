package history

import (
	"bytes"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

var t0 = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

func val(p Point) float64 {
	if p.V == nil {
		return math.NaN()
	}
	return *p.V
}

func last(pts []Point) Point { return pts[len(pts)-1] }

func TestMaxPerBucket(t *testing.T) {
	s := New()
	for i, v := range []float64{10, 80, 30} {
		s.Add(t0.Add(time.Duration(i)*10*time.Second), map[string]float64{"cpu.usage": v})
	}
	pts := s.Series("cpu.usage", 24*time.Hour, t0.Add(30*time.Second))
	if len(pts) != 1440 || val(last(pts)) != 80 || !last(pts).T.Equal(t0) {
		t.Fatalf("last point = %+v (%d points)", last(pts), len(pts))
	}
	if pts[0].V != nil {
		t.Fatal("empty bucket must be nil")
	}
	pts = s.Series("cpu.usage", 7*24*time.Hour, t0)
	if val(last(pts)) != 80 {
		t.Fatalf("10-min bucket = %v", val(last(pts)))
	}
}

func TestRingWraps(t *testing.T) {
	s := New()
	s.Add(t0, map[string]float64{"cpu.usage": 99})
	later := t0.Add(1440 * time.Minute)
	s.Add(later, map[string]float64{"cpu.usage": 5})
	pts := s.Series("cpu.usage", 24*time.Hour, later)
	if val(last(pts)) != 5 {
		t.Fatalf("reused slot = %v, want 5 (old value must be cleared)", val(last(pts)))
	}
	for _, p := range pts[:len(pts)-1] {
		if p.V != nil {
			t.Fatalf("stale value %v at %v", *p.V, p.T)
		}
	}
}

func TestSeriesPicksResolution(t *testing.T) {
	s := New()
	if pts := s.Series("cpu.usage", 24*time.Hour, t0); len(pts) != 1440 || pts[1].T.Sub(pts[0].T) != time.Minute {
		t.Fatalf("24h = %d points", len(pts))
	}
	if pts := s.Series("cpu.usage", 7*24*time.Hour, t0); len(pts) != 1008 || pts[1].T.Sub(pts[0].T) != 10*time.Minute {
		t.Fatalf("7d = %d points", len(pts))
	}
	if pts := s.Series("cpu.usage", 30*24*time.Hour, t0); len(pts) != 720 || pts[1].T.Sub(pts[0].T) != time.Hour {
		t.Fatalf("30d = %d points", len(pts))
	}
	if pts := s.Series("nope", 24*time.Hour, t0); pts != nil {
		t.Fatal("unknown metric must be nil")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "history.v1")
	s := New()
	s.Add(t0, map[string]float64{"cpu.usage": 42.5, "mem.used_pct": 37})
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temp files left: %v", entries)
	}
	l, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	pts := l.Series("cpu.usage", 24*time.Hour, t0)
	if val(last(pts)) != 42.5 || l.Series("gpu.temp", 24*time.Hour, t0)[1439].V != nil {
		t.Fatalf("round trip = %v", val(last(pts)))
	}
	if m, err := Load(filepath.Join(dir, "missing")); err != nil || m == nil {
		t.Fatalf("missing file = %v %v", m, err)
	}
}

func TestCorruptFile(t *testing.T) {
	good := New()
	good.Add(t0, map[string]float64{"cpu.usage": 1})
	var buf bytes.Buffer
	if err := good.write(&buf); err != nil {
		t.Fatal(err)
	}
	valid := buf.Bytes()
	otherMetrics := append([]byte{}, valid...)
	otherMetrics[11] = 'X' // primera letra del primer nombre de métrica
	cases := map[string][]byte{
		"magic":     append([]byte("BADMAGIC"), valid[8:]...),
		"truncated": valid[:len(valid)/2],
		"metrics":   otherMetrics,
		"empty":     {},
	}
	for name, data := range cases {
		dir := t.TempDir()
		p := filepath.Join(dir, "history.v1")
		os.WriteFile(p, data, 0o644)
		s, err := Load(p)
		if !errors.Is(err, ErrCorrupt) || s == nil {
			t.Errorf("%s: err = %v", name, err)
			continue
		}
		if _, err := os.Stat(p + ".bad"); err != nil {
			t.Errorf("%s: no .bad file", name)
		}
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s: corrupt file still in place", name)
		}
		if s.Series("cpu.usage", 24*time.Hour, t0)[1439].V != nil {
			t.Errorf("%s: store not empty", name)
		}
	}
}

func TestExportCSV(t *testing.T) {
	v := 12.5
	var b bytes.Buffer
	WriteCSV(&b, "cpu.usage", []Point{{T: t0, V: &v}, {T: t0.Add(time.Minute)}})
	want := "time,cpu.usage\n2026-09-23T12:00:00Z,12.5\n2026-09-23T12:01:00Z,\n"
	if b.String() != want {
		t.Fatalf("csv = %q", b.String())
	}
	b.Reset()
	WriteJSON(&b, "cpu.usage", []Point{{T: t0, V: &v}, {T: t0.Add(time.Minute)}})
	if !strings.Contains(b.String(), `"metric":"cpu.usage"`) || !strings.Contains(b.String(), `{"t":"2026-09-23T12:01:00Z","v":null}`) {
		t.Fatalf("json = %s", b.String())
	}
}

func f(v float64) *float64 { return &v }

func TestValues(t *testing.T) {
	snap := &model.Snapshot{
		CPU:    &model.CPU{UsagePct: f(23), TempC: f(41), Load: [3]float64{2.1, 1, 0.5}},
		Memory: &model.Memory{TotalB: 100, UsedPct: 38, SwapTotalB: 200, SwapUsedB: 50},
		GPUs: []model.GPU{{Integrated: true, UsagePct: f(99)},
			{UsagePct: f(9), TempC: f(48), VRAMUsedB: u(1 << 30), VRAMTotalB: u(4 << 30), PowerW: f(24)}},
		Storage: &model.Storage{Disks: []model.Disk{{ReadBps: f(100), WriteBps: f(5)}, {ReadBps: f(50)}}},
		Network: &model.Network{RxBps: 2000, TxBps: 100},
		Power:   &model.Power{RAPL: []model.PowerDomain{{Name: "package-0", Watts: 65}}, Batteries: []model.Battery{{Peripheral: true, Pct: 10}, {Pct: 76}}},
	}
	v := Values(snap)
	want := map[string]float64{"cpu.usage": 23, "cpu.temp": 41, "mem.used_pct": 38, "swap.used_pct": 25, "gpu.usage": 9, "gpu.temp": 48,
		"gpu.vram_pct": 25, "gpu.power": 24, "disk.read": 150, "disk.write": 5, "net.rx": 2000, "net.tx": 100, "load1": 2.1, "bat.pct": 76, "power.w": 65}
	for k, w := range want {
		if v[k] != w {
			t.Errorf("%s = %v, want %v", k, v[k], w)
		}
	}
	if len(v) != len(want) {
		t.Errorf("values = %v", v)
	}
	if v := Values(&model.Snapshot{CPU: &model.CPU{}}); len(v) != 1 {
		t.Errorf("missing data must be absent, got %v", v)
	}
}

func u(v uint64) *uint64 { return &v }

func TestSizeBudget(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h")
	New().Save(p)
	st, _ := os.Stat(p)
	if st.Size() > 20<<20 || st.Size() > 250<<10 {
		t.Fatalf("size = %d", st.Size())
	}
}
