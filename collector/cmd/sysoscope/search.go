package main

import (
	"flag"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/view"
)

// searchBox guarda la última búsqueda pedida por el panel hasta la próxima
// línea del stream: sin muestra todavía espera, y una nueva reemplaza a la
// que esperaba.
type searchBox struct {
	query string
	asked bool
}

func (b *searchBox) ask(q string) { b.query, b.asked = q, true }

// take devuelve la respuesta a la búsqueda pedida sobre la muestra s (una
// sola vez), o nil si no hay pedido o todavía no hay muestra.
func (b *searchBox) take(s *model.Snapshot, o view.Options) *view.SearchResult {
	if !b.asked || s == nil {
		return nil
	}
	b.asked = false
	return &view.SearchResult{Query: b.query, Hits: view.Search(s, o, b.query)}
}

// searchCmd es `sysoscope search CONSULTA…`: busca en el contenido de las
// secciones con una muestra, como sections.
func (a *app) searchCmd(args []string) int {
	var words []string
	for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		words, args = append(words, args[0]), args[1:]
	}
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	vf := addViewFlags(fs)
	pretty := fs.Bool("pretty", false, "indent the JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	a.setLang(*vf.lang)
	q := strings.TrimSpace(strings.Join(append(words, fs.Args()...), " "))
	if q == "" {
		return a.fail("usage: sysoscope search QUERY [--lang en|es] [--units c|f] [--pretty]")
	}
	o := a.viewOptions(vf)
	snap, eng := a.fresh("search")
	if eng != nil {
		o.Recent = eng.Recent
		defer eng.Close()
	}
	return a.writeJSON(a.stdout, view.SearchResult{Query: q, Hits: view.Search(snap, o, q)}, *pretty)
}
