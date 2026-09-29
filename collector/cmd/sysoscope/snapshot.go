package main

import (
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/engine"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/system"
)

const latestMaxAge = 5 * time.Second

var sections = map[string]string{"cpu": "cpu", "memory": "memory", "gpu": "gpus", "storage": "storage",
	"network": "network", "sensors": "sensors", "power": "power", "system": "system", "firmware": "firmware", "self": "self", "maintenance": "maintenance", "security": "security"}

func (a *app) snapshot(args []string) int {
	fs := flag.NewFlagSet("snapshot", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	section := fs.String("section", "", "only one section")
	pretty := fs.Bool("pretty", false, "indent the JSON")
	lang := fs.String("lang", "", "en or es")
	fs.Bool("json", true, "JSON output (the only format; accepted for compatibility)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	a.setLang(*lang)
	if _, ok := sections[*section]; *section != "" && !ok {
		return a.fail("unknown section %q", *section)
	}
	// latest.json no guarda lo que identifica (ver ondisk): solo sirve a
	// las secciones que no pierden nada al guardarse.
	var snap *model.Snapshot
	if fromLatest[*section] {
		snap = a.latest()
	}
	if snap == nil {
		focus := ""
		if *section == "gpu" || *section == "maintenance" || *section == "security" || *section == "power" {
			focus = *section
		}
		eng := a.newEngine(a.env, engine.Options{Interval: 2 * time.Second, Focus: focus})
		defer eng.Close()
		eng.Tick()
		if *section == "firmware" {
			eng.CheckFirmware()
			for i := 0; i < 300 && eng.FirmwareRunning(); i++ {
				a.sleep(100 * time.Millisecond)
			}
		} else {
			a.sleep(250 * time.Millisecond)
		}
		snap = eng.Tick()
	}
	var out any = snap
	if *section != "" {
		out = sectionOf(snap, *section)
	}
	return a.writeJSON(a.stdout, out, *pretty)
}

// fromLatest son las secciones de snapshot que latest.json tiene enteras.
var fromLatest = map[string]bool{"cpu": true, "memory": true, "gpu": true, "sensors": true, "self": true}

// latest devuelve el latest.json del stream si tiene menos de 5 s y el mismo
// schema y el mismo idioma (los mensajes de unavailable vienen traducidos).
func (a *app) latest() *model.Snapshot {
	p := filepath.Join(a.env.State, "latest.json")
	st, err := os.Stat(p)
	if err != nil || a.env.Now().Sub(st.ModTime()) > latestMaxAge || a.env.Now().Before(st.ModTime().Add(-time.Second)) {
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var s model.Snapshot
	if json.Unmarshal(data, &s) != nil || s.Schema != model.Schema || s.Lang != a.env.Lang {
		return nil
	}
	if s.System != nil {
		s.System.Hostname = system.Hostname(a.env) // no se guarda: se relee (un archivo)
	}
	return &s
}

// sectionOf arma {"schema", "time", <sección>, "unavailable" de esa sección}.
func sectionOf(s *model.Snapshot, section string) map[string]any {
	full := map[string]json.RawMessage{}
	data, _ := json.Marshal(s)
	json.Unmarshal(data, &full)
	key := sections[section]
	out := map[string]any{"schema": s.Schema, "lang": s.Lang, "time": s.Time}
	if v, ok := full[key]; ok {
		out[key] = v
	}
	prefix := map[string]string{"gpu": "gpu", "self": "self"}[section]
	if prefix == "" {
		prefix = section
	}
	var un []model.Unavailable
	for _, u := range s.Unavailable {
		if u.What == prefix || strings.HasPrefix(u.What, prefix+".") {
			un = append(un, u)
		}
	}
	if len(un) > 0 {
		out["unavailable"] = un
	}
	return out
}

func (a *app) writeJSON(w io.Writer, v any, pretty bool) int {
	enc := json.NewEncoder(w)
	if pretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(v); err != nil {
		return a.fail("could not write JSON: %s", err)
	}
	return 0
}
