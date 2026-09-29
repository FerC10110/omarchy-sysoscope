package view

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestStatJSON(t *testing.T) {
	b := Stat("CPU", "7", "%", "ok", "34 °C").WithValues([]float64{1, 2}).WithValue(1.7).WithLink("cpu")
	j, _ := json.Marshal(b)
	for _, want := range []string{`"type":"stat"`, `"label":"CPU"`, `"text":"7"`, `"unit":"%"`,
		`"lines":["34 °C"]`, `"values":[1,2]`, `"value":1`, `"link":"cpu"`, `"level":"ok"`} {
		if !strings.Contains(string(j), want) {
			t.Errorf("%s lacks %s", j, want)
		}
	}
}

func TestWithDoesNotTouchTheOriginal(t *testing.T) {
	a := Stat("CPU", "7", "%", "ok")
	_ = a.WithLink("cpu")
	if a.Link != "" {
		t.Fatal("WithLink changed the original block")
	}
}

func TestCardsAndHero(t *testing.T) {
	s := Stat("CPU", "7", "%", "ok")
	if c := Cards(s, s); c.Type != "cards" || len(c.Blocks) != 2 {
		t.Errorf("cards = %+v", c)
	}
	h := Hero([]Block{s}, []string{"2.32 GHz"}, []float64{3})
	if h.Type != "hero" || len(h.Blocks) != 1 || h.Lines[0] != "2.32 GHz" || h.Values[0] != 3 {
		t.Errorf("hero = %+v", h)
	}
}

// sample devuelve un bloque de cada tipo, para probar Plain con todos.
func sample(typ string) Block {
	s := Stat("CPU", "7", "%", "ok", "34 °C")
	switch typ {
	case "kv":
		return KV("Processor", [2]string{"Model", "Ryzen"})
	case "meter":
		return Meter("Used", 0.3, "30%", "ok")
	case "spark":
		return Spark("Download", []float64{1, 5, 3}, "1.0 kB/s")
	case "table":
		return Table("Filesystems", []string{"Mount", "Size"}, []Row{{ID: "/", Cells: []string{"/", "1 TiB"}}}, false)
	case "text":
		return Text("All clear", "")
	case "group":
		return Group("enp5s0", s)
	case "cores":
		return Cores("CCX 1", []Cell{{Label: "0", Value: 0.5, Level: "ok"}, {Label: "1", Value: 0, Level: "ok"}})
	case "stat":
		return s
	case "cards":
		return Cards(s, s)
	case "hero":
		return Hero([]Block{s}, []string{"2.32 GHz"}, nil)
	}
	return Block{Type: typ}
}

func TestPlainCoversEveryType(t *testing.T) {
	for _, typ := range PlainTypes {
		lines := Plain(sample(typ))
		if len(lines) == 0 || strings.TrimSpace(strings.Join(lines, "")) == "" {
			t.Errorf("%s has no plain form", typ)
		}
	}
}

func TestPlainForms(t *testing.T) {
	if got := Plain(sample("stat")); !slices.Equal(got, []string{"CPU  7%  34 °C"}) {
		t.Errorf("stat = %q", got)
	}
	if got := Plain(sample("hero")); !slices.Equal(got, []string{"CPU  7%  34 °C · 2.32 GHz"}) {
		t.Errorf("hero = %q", got)
	}
	if got := Plain(sample("cards")); len(got) != 2 {
		t.Errorf("cards = %q, want one line per card", got)
	}
	if got := Plain(sample("meter"))[0]; got != "Used  ██░░░░░░ 30%" {
		t.Errorf("meter = %q", got)
	}
	if got := Plain(Spark("", nil, "")); len(got) != 1 {
		t.Errorf("empty spark = %q", got)
	}
}

// Todo tipo que arman las vistas reales tiene forma simple (regla de §3.4).
func TestEveryViewTypeHasAPlainForm(t *testing.T) {
	for _, fx := range []string{"threadripper", "laptop", "minimal", "optimus", "amdapu"} {
		for _, id := range IDs {
			v := build(t, id, fx, Options{})
			walk(v.Blocks, func(b Block) {
				if !slices.Contains(PlainTypes, b.Type) {
					t.Errorf("%s/%s: type %q is not in PlainTypes", fx, id, b.Type)
				}
			})
		}
	}
}
