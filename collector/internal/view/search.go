package view

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

// Hit es un resultado de la búsqueda por contenido: la sección, la ruta de
// títulos de bloque desde la sección, el texto que coincidió y, si es una
// fila de una tabla que se puede elegir, su id.
type Hit struct {
	Section      string `json:"section"`
	SectionTitle string `json:"section_title"`
	Path         string `json:"path,omitempty"`
	Text         string `json:"text"`
	Row          string `json:"row,omitempty"`
}

// SearchResult es la respuesta a una búsqueda (stream y CLI).
type SearchResult struct {
	Query string `json:"query"`
	Hits  []Hit  `json:"hits"`
}

const (
	searchMax      = 50 // resultados como mucho
	searchMinRunes = 2  // una consulta más corta no busca
	hitRunes       = 80 // largo máximo del texto de un resultado
)

// searchSkip son las secciones que no se buscan: el Resumen repite las demás.
var searchSkip = map[string]bool{"summary": true}

// Search busca query (sin mayúsculas ni acentos) en los bloques de la vista
// de cada sección, armadas en memoria con la muestra s: sin E/S. Devuelve
// hasta searchMax resultados en el orden de las secciones y de los bloques;
// nunca nil.
func Search(s *model.Snapshot, o Options, query string) []Hit {
	hits := []Hit{}
	q := fold(strings.TrimSpace(query))
	if s == nil || utf8.RuneCountInString(q) < searchMinRunes {
		return hits
	}
	for _, id := range IDs {
		if searchSkip[id] {
			continue
		}
		v := buildSafe(id, s, o)
		if v == nil {
			continue
		}
		w := searchWalk{q: q, section: id, title: v.Title, hits: hits}
		w.blocks(v.Blocks, nil)
		hits = w.hits
		if len(hits) >= searchMax {
			return hits[:searchMax]
		}
	}
	return hits
}

// buildSafe arma una vista; si falla o entra en pánico devuelve nil: una
// sección rota se saltea, nunca rompe la búsqueda.
func buildSafe(id string, s *model.Snapshot, o Options) (v *View) {
	defer func() {
		if recover() != nil {
			v = nil
		}
	}()
	v, err := Build(id, s, o)
	if err != nil {
		return nil
	}
	return v
}

type searchWalk struct {
	q, section, title string
	hits              []Hit
}

func (w *searchWalk) full() bool { return len(w.hits) >= searchMax }

// match dice si alguno de los textos contiene la consulta.
func (w *searchWalk) match(ss ...string) bool {
	for _, s := range ss {
		if s != "" && strings.Contains(fold(s), w.q) {
			return true
		}
	}
	return false
}

func (w *searchWalk) add(path []string, text, row string) {
	if w.full() {
		return
	}
	w.hits = append(w.hits, Hit{Section: w.section, SectionTitle: w.title, Path: strings.Join(path, " › "),
		Text: clipRunes(text, hitRunes), Row: row})
}

// blocks recorre bs; path son los títulos de bloque de arriba.
func (w *searchWalk) blocks(bs []Block, path []string) {
	for _, b := range bs {
		if w.full() {
			return
		}
		inner := path
		if b.Title != "" {
			if w.match(b.Title) {
				w.add(path, b.Title, "")
			}
			inner = append(slices.Clip(path), b.Title)
		}
		switch b.Type {
		case "kv":
			rs, _ := b.Rows.([][2]string)
			for _, r := range rs {
				if w.match(r[0], r[1]) {
					w.add(inner, join(": ", r[0], r[1]), "")
				}
			}
		case "table":
			rs, _ := b.Rows.([]Row)
			for _, r := range rs {
				if w.match(r.Cells...) {
					row := ""
					if b.Selectable {
						row = r.ID
					}
					w.add(inner, join(" · ", r.Cells...), row)
				}
			}
		case "stat":
			if w.match(append([]string{b.Label, b.Text}, b.Lines...)...) {
				w.add(inner, join(" ", b.Label, b.Text+b.unitSuffix(), strings.Join(b.Lines, " · ")), "")
			}
		case "meter", "spark":
			if w.match(b.Label, b.Text) {
				w.add(inner, join(" ", b.Label, b.Text), "")
			}
		case "text":
			if w.match(b.Text) {
				w.add(inner, b.Text, "")
			}
		case "hero":
			for _, l := range b.Lines {
				if w.match(l) {
					w.add(inner, l, "")
				}
			}
		}
		w.blocks(b.Blocks, inner) // group, cards y los stat de un hero
	}
}

// unaccent lleva las letras acentuadas del latín a su base (lo que en el
// panel hace normalize("NFD") y sacar las marcas).
var unaccent = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ä': 'a', 'ã': 'a', 'å': 'a',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i',
	'ó': 'o', 'ò': 'o', 'ô': 'o', 'ö': 'o', 'õ': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u',
	'ñ': 'n', 'ç': 'c', 'ý': 'y', 'ÿ': 'y',
}

// fold pasa a minúsculas y saca los acentos: "Energía" → "energia", igual
// que fold de PanelModel.js.
func fold(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= 0x300 && r <= 0x36f { // diacríticos combinantes
			continue
		}
		if m, ok := unaccent[r]; ok {
			r = m
		}
		b.WriteRune(r)
	}
	return b.String()
}

// clipRunes recorta s a n runas, la última "…".
func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}
