package sensors

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

func chip(t *testing.T, s *model.Sensors, id string) model.Chip {
	t.Helper()
	for _, c := range s.Chips {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("chip %s not found in %+v", id, s.Chips)
	return model.Chip{}
}

func TestReadThreadripper(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	r := &collect.Report{Lang: "en"}
	s := Read(env, r)
	if len(s.Chips) != 7 {
		t.Fatalf("chips = %d, want 7", len(s.Chips))
	}
	if s.Chips[0].ID != "hwmon0" || s.Chips[6].ID != "hwmon10" {
		t.Fatalf("order = %s … %s", s.Chips[0].ID, s.Chips[6].ID)
	}
	c := chip(t, s, "hwmon2")
	if c.Kind != "cpu" || c.Label != "CPU (die 0)" {
		t.Errorf("hwmon2 = %s %q", c.Kind, c.Label)
	}
	if c := chip(t, s, "hwmon3"); c.Label != "CPU (die 1)" {
		t.Errorf("hwmon3 label = %q", c.Label)
	}
	n := chip(t, s, "hwmon0")
	if n.Kind != "nvme" || n.Label != "NVMe Samsung SSD 960 PRO 512GB" {
		t.Errorf("hwmon0 = %s %q", n.Kind, n.Label)
	}
	if n.Temps[0].Label != "Composite" || n.Temps[0].Value != 18.85 || n.Temps[0].Crit == nil || *n.Temps[0].Crit != 84.85 || *n.Temps[0].High != 81.85 {
		t.Errorf("composite = %+v", n.Temps[0])
	}
	if c := chip(t, s, "hwmon4"); c.Kind != "ram" || c.Label != "RAM 1" || c.Temps[0].Label != "temp1" || c.Temps[0].Value != 28.75 {
		t.Errorf("hwmon4 = %+v", c)
	}
	b := chip(t, s, "hwmon8")
	if b.Kind != "board" || b.Label != "Motherboard" {
		t.Errorf("hwmon8 = %s %q", b.Kind, b.Label)
	}
	if len(b.Fans) != 3 || b.Fans[0].Label != "CPU Fan" || b.Fans[0].Value != 1117 {
		t.Errorf("fans = %+v", b.Fans)
	}
	if len(b.Volts) != 1 || b.Volts[0].Value != 1.177 {
		t.Errorf("volts = %+v", b.Volts)
	}
	if c := chip(t, s, "hwmon9"); c.Kind != "nic" || c.Label != "Ethernet enp10s0" {
		t.Errorf("hwmon9 = %s %q", c.Kind, c.Label)
	}
	if c := chip(t, s, "hwmon10"); c.Kind != "wifi" || c.Label != "Wi-Fi" {
		t.Errorf("hwmon10 = %s %q", c.Kind, c.Label)
	}
}

func TestCPUTempPrefersTdie(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	s := Read(env, &collect.Report{Lang: "en"})
	v, label, ok := CPUTemp(s)
	if !ok || v != 28.875 || label != "k10temp Tdie" {
		t.Fatalf("CPUTemp = %v %q %v, want 28.875 k10temp Tdie (never Tctl 55.875)", v, label, ok)
	}
}

func hw(t *testing.T, root, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(root, "sys/class/hwmon", dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCPUTempFallbacks(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  float64
		label string
	}{
		{"tccd", map[string]string{"name": "k10temp", "temp1_label": "Tctl", "temp1_input": "70000", "temp3_label": "Tccd1", "temp3_input": "60000", "temp4_label": "Tccd2", "temp4_input": "62000"}, 62, "k10temp Tccd2"},
		{"tctl only", map[string]string{"name": "k10temp", "temp1_label": "Tctl", "temp1_input": "51000"}, 51, "k10temp Tctl"},
		{"coretemp", map[string]string{"name": "coretemp", "temp1_label": "Package id 0", "temp1_input": "45000", "temp2_label": "Core 0", "temp2_input": "44000"}, 45, "coretemp Package id 0"},
		{"arm", map[string]string{"name": "cpu_thermal", "temp1_input": "52500"}, 52.5, "cpu_thermal temp1"},
	}
	for _, c := range cases {
		root := t.TempDir()
		hw(t, root, "hwmon0", c.files)
		env := source.Env{FS: source.FS{Root: root}, Lang: "en"}
		v, label, ok := CPUTemp(Read(env, &collect.Report{Lang: "en"}))
		if !ok || v != c.want || label != c.label {
			t.Errorf("%s: CPUTemp = %v %q %v, want %v %q", c.name, v, label, ok, c.want, c.label)
		}
	}
}

func TestNoHwmon(t *testing.T) {
	env := source.Env{FS: source.FS{Root: t.TempDir()}, Lang: "en"}
	r := &collect.Report{Lang: "en"}
	s := Read(env, r)
	if s == nil || len(s.Chips) != 0 {
		t.Fatalf("sensors = %+v", s)
	}
	if _, _, ok := CPUTemp(s); ok {
		t.Fatal("CPUTemp must be not ok")
	}
	if len(r.List) != 1 || r.List[0].What != "sensors" || r.List[0].Reason != "not_present" {
		t.Fatalf("unavailable = %+v", r.List)
	}
}
