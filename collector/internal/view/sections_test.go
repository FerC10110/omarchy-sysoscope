package view

import (
	"slices"
	"testing"
)

func TestSectionsThreadripper(t *testing.T) {
	s, _ := snap(t, "threadripper")
	secs := Sections(s, Options{Lang: "en"})
	if len(secs) != len(IDs) || len(IDs) != 13 {
		t.Fatalf("sections = %d, ids = %d", len(secs), len(IDs))
	}
	by := map[string]Section{}
	for i, sec := range secs {
		if sec.ID != IDs[i] {
			t.Fatalf("section %d = %q, want %q", i, sec.ID, IDs[i])
		}
		by[sec.ID] = sec
	}
	if by["about"].Badge != 1 || by["storage"].Badge != 2 {
		t.Errorf("badges: about %d (1 failed unit), storage %d (2 disks with bad sectors)", by["about"].Badge, by["storage"].Badge)
	}
	if by["cpu"].Group != "components" || by["about"].Group != "system" || by["summary"].Group != "" {
		t.Errorf("groups = %+v", secs)
	}
	if by["maintenance"].Group != "system" || by["maintenance"].Badge != 0 || IDs[9] != "maintenance" {
		t.Errorf("maintenance = %+v at %d", by["maintenance"], slices.Index(IDs, "maintenance"))
	}
	es := Sections(s, Options{Lang: "es"})
	if es[4].Title != "Discos" || es[8].Title != "Acerca de" || es[9].Title != "Mantenimiento" {
		t.Errorf("es titles: %q %q %q", es[4].Title, es[8].Title, es[9].Title)
	}
}

func TestBuildUnknown(t *testing.T) {
	s, _ := snap(t, "threadripper")
	if _, err := Build("nope", s, Options{Lang: "en"}); err == nil {
		t.Fatal("unknown section must fail")
	}
}

func TestUnavailableBlock(t *testing.T) {
	s, _ := snap(t, "minimal")
	b := Unavailable(s, "sensors")
	if b == nil || b.Type != "text" || b.Level != "warn" || b.Text != "not available: not present on this system" {
		t.Fatalf("sensors unavailable = %+v", b)
	}
	if Unavailable(s, "cpu.model") != nil {
		t.Fatal("no entry must give nil")
	}
}
