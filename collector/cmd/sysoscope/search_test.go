package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/view"
)

// El stream responde una búsqueda enseguida, en una línea extra con la
// última muestra, sin tomar otra; la línea siguiente ya no la trae.
func TestStreamSearch(t *testing.T) {
	_, fake, lines, errb := streamLines(t, "search \"fake\"\nsearch nope\nsearch \"a \\\"b\\\"\"\nrefresh\nquit\n")
	if len(lines) != 4 || fake.ticks != 2 {
		t.Fatalf("%d lines, %d ticks: %q", len(lines), fake.ticks, lines)
	}
	var got struct {
		Search *view.SearchResult `json:"search"`
		CPU    *model.CPU         `json:"cpu"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &got); err != nil || got.Search == nil || got.CPU == nil {
		t.Fatalf("line 1 = %s (%v)", lines[1], err)
	}
	if got.Search.Query != "fake" || len(got.Search.Hits) == 0 || got.Search.Hits[0].Section != "cpu" ||
		!strings.Contains(got.Search.Hits[0].Text, "Fake") {
		t.Errorf("search = %+v", got.Search)
	}
	if !strings.Contains(errb, `usage: search "QUERY" (a JSON string)`) {
		t.Errorf("stderr = %q", errb)
	}
	got.Search = nil
	json.Unmarshal([]byte(lines[2]), &got)
	if got.Search == nil || got.Search.Query != `a "b"` || got.Search.Hits == nil {
		t.Errorf("quoted = %+v", got.Search)
	}
	if strings.Contains(lines[3], `"search"`) {
		t.Errorf("la respuesta sale una sola vez: %s", lines[3])
	}
}

// Sin muestra todavía, la búsqueda espera; una nueva reemplaza a la vieja.
func TestSearchBoxKeepsTheLast(t *testing.T) {
	var b searchBox
	if b.take(&model.Snapshot{}, view.Options{}) != nil {
		t.Fatal("sin pedido no hay respuesta")
	}
	b.ask("ab")
	b.ask("fake")
	if b.take(nil, view.Options{}) != nil {
		t.Fatal("sin muestra espera")
	}
	s := (&fakeEngine{}).Tick() // CPU "Fake"
	r := b.take(s, view.Options{Lang: "en"})
	if r == nil || r.Query != "fake" || len(r.Hits) == 0 {
		t.Fatalf("take = %+v", r)
	}
	if b.take(s, view.Options{Lang: "en"}) != nil {
		t.Error("se entrega una sola vez")
	}
}

func TestSearchCmd(t *testing.T) {
	a, _, out, errb := newApp(t, "")
	if code := a.run([]string{"search", "fa", "ke", "--lang", "en"}); code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	var r view.SearchResult
	if err := json.Unmarshal(out.Bytes(), &r); err != nil || r.Query != "fa ke" || r.Hits == nil {
		t.Fatalf("out = %s (%v)", out.String(), err)
	}
	out.Reset()
	if code := a.run([]string{"search", "fake"}); code != 0 || !strings.Contains(out.String(), `"section":"cpu"`) {
		t.Fatalf("fake exit %d: %s", code, out.String())
	}
	out.Reset()
	if code := a.run([]string{"search", "x"}); code != 0 || !strings.Contains(out.String(), `"hits":[]`) {
		t.Errorf("consulta corta = %s", out.String())
	}
	if code := a.run([]string{"search"}); code != 2 || !strings.Contains(errb.String(), "usage: sysoscope search QUERY") {
		t.Errorf("sin consulta: exit %d, stderr %q", code, errb.String())
	}
}
