package power

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// copyRoot copia el root de un fixture a un directorio temporal.
func copyRoot(t *testing.T, env *source.Env) string {
	t.Helper()
	dst := t.TempDir()
	if err := os.CopyFS(dst, os.DirFS(env.FS.Root)); err != nil {
		t.Fatal(err)
	}
	env.FS.Root = dst
	return dst
}

func TestLaptop(t *testing.T) {
	env := source.Fixture(t, "laptop")
	now := source.FixtureNow
	env.Now = func() time.Time { return now }
	root := copyRoot(t, &env)
	r := &collect.Report{Lang: "en"}
	p := &model.Power{}
	var s Sampler
	s.Sample(env, p, true, r)
	if len(p.Batteries) != 2 {
		t.Fatalf("batteries = %+v", p.Batteries)
	}
	b := p.Batteries[0]
	if b.Name != "BAT0" || b.Pct != 76 || b.Peripheral || b.Status != "Discharging" {
		t.Errorf("BAT0 = %+v", b)
	}
	if b.HealthPct == nil || math.Abs(*b.HealthPct-50.0/57*100) > 1e-9 || *b.Cycles != 212 || *b.PowerW != 9.5 || *b.TimeLeftS != 14400 || *b.ChargeLimitPct != 80 {
		t.Errorf("BAT0 details = health %v cycles %v power %v left %v limit %v", b.HealthPct, b.Cycles, b.PowerW, b.TimeLeftS, b.ChargeLimitPct)
	}
	if *b.FullWh != 50 || *b.DesignWh != 57 {
		t.Errorf("Wh = %v/%v", *b.FullWh, *b.DesignWh)
	}
	m := p.Batteries[1]
	if !m.Peripheral || m.Model != "MX Master 3" || m.Pct != 55 || m.HealthPct != nil {
		t.Errorf("mouse = %+v", m)
	}
	if p.OnAC == nil || *p.OnAC {
		t.Errorf("on ac = %v", p.OnAC)
	}
	if len(p.RAPL) != 0 {
		t.Errorf("RAPL needs two samples: %+v", p.RAPL)
	}
	os.WriteFile(filepath.Join(root, "sys/class/powercap/intel-rapl:0/energy_uj"), []byte("31000000\n"), 0o644)
	now = now.Add(2 * time.Second)
	s.Sample(env, p, false, r)
	if len(p.RAPL) != 1 || p.RAPL[0] != (model.PowerDomain{Name: "package-0", Watts: 15}) {
		t.Errorf("rapl = %+v", p.RAPL)
	}
	for _, u := range r.List {
		if u.What != "power.profile" {
			t.Errorf("unexpected unavailable %+v", u)
		}
	}
}

func TestRAPLWrap(t *testing.T) {
	if w := watts(262143328850-1000000, 29000000, 262143328850, 2); w != 15 {
		t.Fatalf("wrap watts = %v", w)
	}
}

func TestChargingTimeLeft(t *testing.T) {
	env := source.Fixture(t, "laptop")
	root := copyRoot(t, &env)
	os.WriteFile(filepath.Join(root, "sys/class/power_supply/BAT0/status"), []byte("Charging\n"), 0o644)
	p := &model.Power{}
	var s Sampler
	s.Sample(env, p, false, &collect.Report{Lang: "en"})
	if b := p.Batteries[0]; b.TimeLeftS == nil || *b.TimeLeftS != 4547 {
		t.Fatalf("time to full = %v, want (50-38)/9.5 h", b.TimeLeftS)
	}
}

func TestThreadripper(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	root := copyRoot(t, &env)
	if os.Geteuid() == 0 {
		t.Skip("root can read a 000 file")
	}
	os.Chmod(filepath.Join(root, "sys/class/powercap/intel-rapl:0/energy_uj"), 0)
	r := &collect.Report{Lang: "en"}
	p := &model.Power{}
	var s Sampler
	s.Sample(env, p, true, r)
	if len(p.Batteries) != 0 || p.OnAC != nil || len(p.RAPL) != 0 {
		t.Errorf("desktop power = %+v", p)
	}
	if p.Profile != "balanced" || !reflect.DeepEqual(p.Profiles, []string{"power-saver", "balanced"}) {
		t.Errorf("profile = %q %v", p.Profile, p.Profiles)
	}
	if len(r.List) != 1 || r.List[0].What != "power.rapl" || r.List[0].Reason != "permission" ||
		r.List[0].Message != "permission denied: locked by the kernel; can be unlocked from Settings (stage E)" {
		t.Fatalf("unavailable = %+v", r.List)
	}
	s.Sample(env, p, false, r)
	if p.Profile != "balanced" {
		t.Error("fast tick must keep the profile")
	}
}

func TestNoPPD(t *testing.T) {
	env := source.Fixture(t, "laptop")
	r := &collect.Report{Lang: "en"}
	p := &model.Power{}
	var s Sampler
	s.Sample(env, p, true, r)
	if p.Profile != "" || len(r.List) != 1 || r.List[0].What != "power.profile" || r.List[0].Reason != "missing_tool" {
		t.Fatalf("profile %q unavailable %+v", p.Profile, r.List)
	}
}
