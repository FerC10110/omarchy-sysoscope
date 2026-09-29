package main

import (
	"encoding/json"
	"flag"
	"slices"
	"strings"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/engine"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/view"
)

// engineFocus es el foco del engine para una vista: solo algunas leen más.
func engineFocus(id string) string {
	switch id {
	case "cpu", "memory", "gpu", "network", "sensors", "security", "apps", "power", "devices", "maintenance":
		return id
	}
	return ""
}

// viewFlags son las opciones de presentación de view, sections y stream.
type viewFlags struct {
	lang, units, cards *string
}

func addViewFlags(fs *flag.FlagSet) viewFlags {
	return viewFlags{
		lang:  fs.String("lang", "", "en or es"),
		units: fs.String("units", "c", "temperature unit: c or f"),
		cards: fs.String("cards", "", "summary cards, comma-separated"),
	}
}

func splitCards(s string) []string {
	var out []string
	for _, c := range strings.Split(s, ",") {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return out
}

func (a *app) viewOptions(f viewFlags) view.Options {
	return view.Options{Lang: a.env.Lang, Units: *f.units, Cards: splitCards(*f.cards)}
}

// fresh devuelve latest.json si sirve para la vista id ("" = la lista de
// secciones, "search" = la búsqueda) o, si no, el segundo tick de un engine
// con el foco de esa vista (y el engine, para los valores recientes).
func (a *app) fresh(id string) (*model.Snapshot, sampler) {
	if s := a.latest(); s != nil && latestServes(s, id) {
		return s, nil
	}
	eng := a.newEngine(a.env, engine.Options{Interval: 2 * time.Second, Focus: engineFocus(id)})
	eng.Tick()
	a.sleep(250 * time.Millisecond)
	return eng.Tick(), eng
}

// latestServes dice si latest.json alcanza para la vista id. latest.json
// no guarda lo que identifica (series, MACs, redes, títulos…: ver ondisk),
// así que solo sirve a lo que no lo muestra: la lista de secciones, el
// Resumen (su hostname se relee al cargarlo) y CPU y Memoria, que además
// necesitan las apps (el stream las lee solo con una vista que las
// muestra). La búsqueda recorre todas las vistas: nunca.
func latestServes(s *model.Snapshot, id string) bool {
	switch id {
	case "", "summary":
		return true
	case "cpu", "memory":
		return s.Apps != nil && len(s.Apps.List) > 0
	}
	return false
}

func (a *app) view(args []string) int {
	id := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("view", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	vf := addViewFlags(fs)
	pretty := fs.Bool("pretty", false, "indent the JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if id == "" {
		id = fs.Arg(0)
	}
	a.setLang(*vf.lang)
	v, ok := a.buildView(id, a.viewOptions(vf))
	if !ok {
		return a.fail("unknown section %q", id)
	}
	return a.writeJSON(a.stdout, v, *pretty)
}

// buildView arma una vista con un snapshot fresco; false si el id no existe.
func (a *app) buildView(id string, o view.Options) (*view.View, bool) {
	if !slices.Contains(view.IDs, id) {
		return nil, false
	}
	snap, eng := a.fresh(id)
	if eng != nil {
		o.Recent = eng.Recent
		defer eng.Close()
	}
	v, err := view.Build(id, snap, o)
	return v, err == nil
}

func (a *app) sections(args []string) int {
	fs := flag.NewFlagSet("sections", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	lang := fs.String("lang", "", "en or es")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	a.setLang(*lang)
	snap, eng := a.fresh("")
	if eng != nil {
		defer eng.Close()
	}
	return a.writeJSON(a.stdout, view.Sections(snap, view.Options{Lang: a.env.Lang}), false)
}

// withView pega "sections" (y "view", "result" y "search", si hay) al JSON
// del snapshot, antes de su "}" final, sin volver a serializarlo.
func withView(snap []byte, secs []view.Section, v *view.View, res *actionResult, srch *view.SearchResult) []byte {
	out := make([]byte, 0, len(snap)+4096)
	out = append(out, snap[:len(snap)-1]...)
	sd, _ := json.Marshal(secs)
	out = append(append(out, `,"sections":`...), sd...)
	if v != nil {
		vd, _ := json.Marshal(v)
		out = append(append(out, `,"view":`...), vd...)
	}
	if res != nil {
		rd, _ := json.Marshal(res)
		out = append(append(out, `,"result":`...), rd...)
	}
	if srch != nil {
		qd, _ := json.Marshal(srch)
		out = append(append(out, `,"search":`...), qd...)
	}
	return append(out, '}')
}
