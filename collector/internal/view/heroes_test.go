package view

import (
	"strings"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

func hero(t *testing.T, v *View) Block {
	t.Helper()
	if len(v.Blocks) == 0 || v.Blocks[0].Type != "hero" {
		t.Fatalf("%s: first block is not a hero: %+v", v.ID, v.Blocks)
	}
	return v.Blocks[0]
}

func TestHeroFirst(t *testing.T) {
	for _, c := range []struct{ id, fx string }{
		{"cpu", "threadripper"}, {"memory", "threadripper"}, {"gpu", "threadripper"},
		{"storage", "threadripper"}, {"network", "threadripper"}, {"sensors", "threadripper"},
		{"power", "laptop"}, {"about", "threadripper"}, {"devices", "threadripper"}, {"security", "threadripper"},
	} {
		h := hero(t, build(t, c.id, c.fx, Options{}))
		if len(h.Blocks) == 0 && len(h.Lines) == 0 {
			t.Errorf("%s: empty hero", c.id)
		}
	}
}

func TestCPUHero(t *testing.T) {
	h := hero(t, build(t, "cpu", "threadripper", Options{}))
	u := h.Blocks[0]
	if u.Type != "stat" || u.Text != "38" || u.Unit != "%" || u.Level != "ok" {
		t.Errorf("usage stat = %+v", u)
	}
	if len(h.Lines) == 0 {
		t.Error("no detail lines")
	}
	es := hero(t, build(t, "cpu", "threadripper", Options{Lang: "es"}))
	if es.Blocks[0].Label != "Uso" {
		t.Errorf("es label = %q", es.Blocks[0].Label)
	}
}

func TestHeroUnitsFollowOptions(t *testing.T) {
	h := hero(t, build(t, "cpu", "threadripper", Options{Units: "f"}))
	for _, s := range h.Blocks {
		if s.Unit == "°C" {
			t.Errorf("°C with units=f: %+v", s)
		}
	}
}

func TestHeroAbsentWithoutData(t *testing.T) {
	s, _ := snap(t, "threadripper")
	empty := &model.Snapshot{Lang: "en", Unavailable: s.Unavailable}
	for _, id := range []string{"cpu", "memory", "gpu", "storage", "network", "sensors", "power", "about", "devices", "security"} {
		v, err := Build(id, empty, Options{Lang: "en"})
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range v.Blocks {
			if b.Type == "hero" {
				t.Errorf("%s: hero without data: %+v", id, b)
			}
		}
	}
}

func TestStorageHeroFill(t *testing.T) {
	h := hero(t, build(t, "storage", "threadripper", Options{}))
	if h.Blocks[0].Value == nil {
		t.Errorf("free-space stat has no fill: %+v", h.Blocks[0])
	}
}

// A source that could not be read is "—", not "0" (the reason is below).
func TestDevicesHeroDashForUnavailable(t *testing.T) {
	h := hero(t, build(t, "devices", "threadripper", Options{}))
	for _, b := range h.Blocks {
		if b.Label == "Bluetooth" && b.Text != "—" {
			t.Errorf("Bluetooth stat = %q, want —", b.Text)
		}
	}
}

// The Summary header keeps the CPU, the GPUs and the memory, each on a line
// wide enough to read (not squeezed under the hostname).
func TestSummaryHeroLinesHaveTheMachine(t *testing.T) {
	h := hero(t, build(t, "summary", "threadripper", Options{}))
	all := strings.Join(h.Lines, "\n")
	for _, want := range []string{"Threadripper", "RTX 3080", "GiB"} {
		if !strings.Contains(all, want) {
			t.Errorf("hero lines %q lack %q", h.Lines, want)
		}
	}
}
