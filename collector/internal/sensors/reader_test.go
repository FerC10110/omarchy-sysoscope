package sensors

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

func TestReaderSkipsSlowChips(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	dst := t.TempDir()
	os.CopyFS(dst, os.DirFS(env.FS.Root))
	env.FS.Root = dst
	now := source.FixtureNow
	env.Now = func() time.Time { return now }
	r := &collect.Report{Lang: "en"}
	rd := &Reader{SlowEvery: 5 * time.Minute}
	rd.Read(env, r)
	rd.cost["hwmon8"] = 50 * time.Millisecond // el WMI de ASUS: ~55 ms por lectura
	set := func(rel, v string) { os.WriteFile(filepath.Join(dst, rel), []byte(v+"\n"), 0o644) }
	set("sys/class/hwmon/hwmon8/fan1_input", "999")
	set("sys/class/hwmon/hwmon2/temp2_input", "40000")

	now = now.Add(6 * time.Second)
	s := rd.Read(env, r)
	if c := chip(t, s, "hwmon8"); c.Fans[0].Value != 1117 {
		t.Errorf("slow chip re-read too soon: fan = %v", c.Fans[0].Value)
	}
	if v, _, _ := CPUTemp(s); v != 40 {
		t.Errorf("cpu chip must be read every time: %v", v)
	}
	rd.Focus = true
	s = rd.Read(env, r)
	if c := chip(t, s, "hwmon8"); c.Fans[0].Value != 999 {
		t.Errorf("with the Sensors section open the slow chip must be live: %v", c.Fans[0].Value)
	}
	rd.Focus = false
	rd.cost["hwmon8"] = 50 * time.Millisecond
	set("sys/class/hwmon/hwmon8/fan1_input", "888")
	now = now.Add(5 * time.Minute)
	if c := chip(t, rd.Read(env, r), "hwmon8"); c.Fans[0].Value != 888 {
		t.Errorf("slow chip not refreshed after SlowEvery: %v", c.Fans[0].Value)
	}
	if len(rd.Read(env, r).Chips) != 7 {
		t.Error("cached chips must stay in the output")
	}
}

func TestSuspendedDeviceChipIsNotRead(t *testing.T) {
	root := t.TempDir()
	for rel, v := range map[string]string{
		"sys/class/hwmon/hwmon0/name":                        "k10temp",
		"sys/class/hwmon/hwmon0/temp1_input":                 "45000",
		"sys/class/hwmon/hwmon3/name":                        "amdgpu",
		"sys/class/hwmon/hwmon3/temp1_input":                 "41000",
		"sys/class/hwmon/hwmon3/device/power/runtime_status": "suspended",
		"sys/class/hwmon/hwmon4/name":                        "nouveau",
		"sys/class/hwmon/hwmon4/temp1_input":                 "39000",
		"sys/class/hwmon/hwmon4/device/power/runtime_status": "active",
	} {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(v+"\n"), 0o644)
	}
	env := source.Env{FS: source.FS{Root: root}, Now: func() time.Time { return source.FixtureNow }}
	s := Read(env, &collect.Report{Lang: "en"})
	var names []string
	for _, c := range s.Chips {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "k10temp,nouveau" {
		t.Fatalf("chips = %v; a runtime-suspended device must not be read (it would wake up)", names)
	}
}

func TestUnsetThresholdIsDropped(t *testing.T) {
	root := t.TempDir()
	for rel, v := range map[string]string{
		"sys/class/hwmon/hwmon1/name":        "nvme",
		"sys/class/hwmon/hwmon1/temp1_input": "19000",
		"sys/class/hwmon/hwmon1/temp1_max":   "72850",
		"sys/class/hwmon/hwmon1/temp1_crit":  "76850",
		"sys/class/hwmon/hwmon1/temp2_input": "19000",
		"sys/class/hwmon/hwmon1/temp2_max":   "65261850",
		"sys/class/hwmon/hwmon1/temp2_min":   "-273150",
	} {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(v+"\n"), 0o644)
	}
	env := source.Env{FS: source.FS{Root: root}, Now: func() time.Time { return source.FixtureNow }}
	s := Read(env, &collect.Report{Lang: "en"})
	temps := s.Chips[0].Temps
	if temps[0].High == nil || *temps[0].High != 72.85 || temps[0].Crit == nil {
		t.Fatalf("real limits lost: %+v", temps[0])
	}
	// nvme reports an unset limit as 65261.85 °C (0xFFFF kelvin): not a limit.
	if temps[1].High != nil {
		t.Fatalf("temp2 high = %v, want none", *temps[1].High)
	}
}
