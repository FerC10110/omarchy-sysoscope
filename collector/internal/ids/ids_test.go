package ids

import (
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

func TestPCI(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	v, d := PCI(env, "10de", "2206")
	if v != "NVIDIA Corporation" || d != "GA102 [GeForce RTX 3080]" {
		t.Fatalf("PCI = %q %q", v, d)
	}
	v, d = PCI(env, "0x1002", "0x7550")
	if v != "Advanced Micro Devices, Inc. [AMD/ATI]" || d != "Navi 48 [Radeon RX 9070/9070 XT/9070 GRE]" {
		t.Fatalf("PCI amd = %q %q", v, d)
	}
	if v, d := PCI(env, "10de", "ffff"); v != "NVIDIA Corporation" || d != "" {
		t.Fatalf("unknown device = %q %q", v, d)
	}
	if v, d := PCI(env, "abcd", "0001"); v != "" || d != "" {
		t.Fatalf("unknown vendor = %q %q", v, d)
	}
	empty := source.Env{FS: source.FS{Root: t.TempDir()}}
	if v, d := PCI(empty, "10de", "2206"); v != "" || d != "" {
		t.Fatalf("no ids file = %q %q", v, d)
	}
}
