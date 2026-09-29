package view

import "testing"

// summaryCards devuelve los bloques del cards del Resumen.
func summaryCards(v *View) []Block {
	for _, b := range v.Blocks {
		if b.Type == "cards" {
			return b.Blocks
		}
	}
	return nil
}

// cardLabels devuelve la etiqueta de cada tarjeta.
func cardLabels(v *View) []string {
	var out []string
	for _, b := range summaryCards(v) {
		out = append(out, b.Label)
	}
	return out
}

func TestSummaryCards(t *testing.T) {
	v := build(t, "summary", "threadripper", Options{})
	h := v.Blocks[0]
	if h.Type != "hero" || len(h.Blocks) != 1 || h.Blocks[0].Text == "" || len(h.Lines) == 0 {
		t.Fatalf("header = %+v", h)
	}
	got := cardLabels(v)
	want := []string{"CPU", "Memory", "NVIDIA GeForce RTX 3080", "Disk /", "Network"}
	if len(got) < len(want) {
		t.Fatalf("cards = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("cards = %q, want %q", got, want)
		}
	}
	for _, c := range summaryCards(v) {
		if c.Type == "stat" && c.Link == "" {
			t.Errorf("card %q has no link", c.Label)
		}
	}
	l := build(t, "summary", "threadripper", Options{Cards: []string{"load", "cpu"}})
	if c := cardLabels(l); c[0] != "Load" {
		t.Errorf("first card = %q, want Load", c[0])
	}
	noEmptyValues(t, v)
}

func TestSummaryDiskCardHasFill(t *testing.T) {
	v := build(t, "summary", "threadripper", Options{})
	found := false
	for _, c := range summaryCards(v) {
		if c.Label == "Disk /" {
			found = true
			if c.Value == nil || c.Unit == "" {
				t.Errorf("disk card = %+v", c)
			}
		}
	}
	if !found {
		t.Error("no Disk / card")
	}
}

func TestSummaryAllClear(t *testing.T) {
	s, _ := snap(t, "threadripper")
	s.System.FailedUnits = nil
	for i := range s.Storage.Disks {
		s.Storage.Disks[i].Smart = nil
	}
	s.Firmware = nil
	v, err := Build("summary", s, Options{Lang: "es"})
	if err != nil {
		t.Fatal(err)
	}
	last := v.Blocks[len(v.Blocks)-1]
	if last.Type != "text" || last.Text != "Todo en orden" {
		t.Errorf("last block = %+v", last)
	}
}

func TestSummaryWarnings(t *testing.T) {
	s, _ := snap(t, "threadripper")
	w := Warnings(s, Options{Lang: "en"})
	if len(w) != 3 {
		t.Fatalf("warnings = %+v, want failed unit + 2 disks with bad sectors", w)
	}
	links := map[string]int{}
	for _, b := range w {
		links[b.Link]++
	}
	if links["about"] != 1 || links["storage"] != 2 {
		t.Errorf("links = %v", links)
	}
	v := build(t, "summary", "threadripper", Options{Lang: "es"})
	last := v.Blocks[len(v.Blocks)-1]
	if last.Type != "group" || last.Title != "Requiere atención" {
		t.Errorf("warnings group = %q", last.Title)
	}
	if secs := Sections(s, Options{Lang: "en"}); secs[0].Badge != 3 {
		t.Errorf("summary badge = %d", secs[0].Badge)
	}
}
