package view

import (
	"strings"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

func cardsOf(v *View) []Block {
	for _, b := range v.Blocks {
		if b.Type == "cards" {
			return b.Blocks
		}
	}
	return nil
}

// A card whose source could not be read still says which card it is.
func TestUnavailableCardHasItsLabel(t *testing.T) {
	s, e := snap(t, "threadripper")
	s.Storage = nil
	s.Unavailable = append(s.Unavailable, model.Unavailable{What: "storage", Reason: "missing_tool", Message: "requires lsblk"})
	v, err := Build("summary", s, Options{Lang: "en", Recent: e.Recent})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cardsOf(v) {
		if c.Label == "Disk /" {
			if c.Type != "stat" || c.Text != "—" || len(c.Lines) == 0 || !strings.Contains(c.Lines[0], "requires lsblk") {
				t.Errorf("unavailable disk card = %+v", c)
			}
			return
		}
	}
	t.Errorf("no card labelled Disk /: %+v", cardsOf(v))
}

// The disk card's number keeps a short unit; "free" goes to the line below.
func TestDiskCardUnitIsShort(t *testing.T) {
	for _, c := range cardsOf(build(t, "summary", "threadripper", Options{})) {
		if c.Label == "Disk /" {
			if strings.Contains(c.Unit, " ") || len(c.Lines) == 0 || !strings.HasPrefix(c.Lines[0], "free") {
				t.Errorf("disk card = unit %q lines %q", c.Unit, c.Lines)
			}
			return
		}
	}
	t.Error("no disk card")
}

// A desktop without a battery or RAPL shows the draw it measured (the GPU).
func TestPowerHeroFallsBackToMeasuredDraw(t *testing.T) {
	h := hero(t, build(t, "power", "threadripper", Options{}))
	if len(h.Blocks) == 0 || h.Blocks[0].Unit != "W" {
		t.Errorf("power hero = %+v", h)
	}
}

// With two awake GPUs, the second one is in the header too.
func TestGPUHeroNamesTheOtherGPUs(t *testing.T) {
	s, e := snap(t, "threadripper")
	g := s.GPUs[0]
	g.Name = "Second GPU"
	s.GPUs = append(s.GPUs, g)
	v, err := Build("gpu", s, Options{Lang: "en", Recent: e.Recent})
	if err != nil {
		t.Fatal(err)
	}
	h := hero(t, v)
	if !strings.Contains(strings.Join(h.Lines, "\n"), "Second GPU") {
		t.Errorf("hero lines %q lack the second GPU", h.Lines)
	}
}

func TestMemoryHeroHasZram(t *testing.T) {
	h := hero(t, build(t, "memory", "threadripper", Options{}))
	if !strings.Contains(strings.Join(h.Lines, "\n"), "zram") {
		t.Errorf("hero lines %q lack zram", h.Lines)
	}
}

func TestNetworkHeroArrows(t *testing.T) {
	h := hero(t, build(t, "network", "threadripper", Options{}))
	if !strings.HasPrefix(h.Blocks[0].Label, "↓") || !strings.HasPrefix(h.Blocks[1].Label, "↑") {
		t.Errorf("labels %q %q", h.Blocks[0].Label, h.Blocks[1].Label)
	}
}

func TestAboutHeroSingularFailedService(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		h := hero(t, build(t, "about", "threadripper", Options{Lang: lang}))
		all := strings.Join(h.Lines, "\n")
		want := map[string]string{"en": "1 failed service", "es": "1 servicio con fallas"}[lang]
		if !strings.Contains(all, want) || strings.Contains(all, want+"s") {
			t.Errorf("%s: lines %q, want %q", lang, h.Lines, want)
		}
	}
}
