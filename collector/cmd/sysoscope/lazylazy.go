package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/engine"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/maint"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/system"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/view"
)

// lazylazy es el protocolo de pestañas de lazylazy (spec §7.3). El idioma
// llega en LAZYLAZY_LANG; unidades y tarjetas son las del panel.
func (a *app) lazylazy(args []string) int {
	a.setLang(os.Getenv("LAZYLAZY_LANG"))
	if len(args) == 0 {
		return a.fail("usage: sysoscope lazylazy sections | view ID | stream ID | action ID")
	}
	o := a.panelOptions()
	switch args[0] {
	case "sections":
		snap, eng := a.fresh("")
		if eng != nil {
			defer eng.Close()
		}
		return a.writeJSON(a.stdout, view.Sections(snap, o), false)
	case "view":
		if len(args) < 2 {
			return a.fail("usage: sysoscope lazylazy sections | view ID | stream ID | action ID")
		}
		v, ok := a.buildView(args[1], o)
		if !ok {
			return a.fail("unknown section %q", args[1])
		}
		return a.writeJSON(a.stdout, forLazylazy(v), false)
	case "stream":
		if len(args) < 2 || !slices.Contains(view.IDs, args[1]) {
			return a.fail("usage: sysoscope lazylazy sections | view ID | stream ID | action ID")
		}
		return a.lazyStream(args[1], o)
	case "action":
		if len(args) >= 2 && args[1] == "firmware.check" {
			return a.firmwareCheck()
		}
		if len(args) >= 2 && args[1] == "updates.check" {
			return a.updatesCheck()
		}
		if len(args) >= 2 && isAction(args[1]) {
			return a.actionCmd(args[1:])
		}
		return a.fail("usage: sysoscope lazylazy sections | view ID | stream ID | action ID")
	}
	return a.fail("usage: sysoscope lazylazy sections | view ID | stream ID | action ID")
}

// forLazylazy pasa a pantalla completa lo que puede tardar más de 5 s o
// necesita la terminal, y ajusta lo que lazylazy no tiene: enter es suyo, y
// sin "hide" una tecla es una sola acción (la c de Wi-Fi queda en
// wifi.connect, que con una red nueva con clave abre nmtui en la terminal y
// con una oculta, wifi-hidden; de las v de Dispositivos, una por fila, queda
// una sola, sin el volumen de una fila como dato por defecto). updates.check
// muestra la lista en la terminal.
func forLazylazy(v *view.View) *view.View {
	out := v.Actions[:0]
	volume := false
	for _, a := range v.Actions {
		switch a.ID {
		case "firmware.check", "updates.check", "wifi.connect":
			a.Mode = "fullscreen"
		case "wifi.connect_new", "wifi.connect_hidden":
			continue
		case "audio.volume":
			if volume {
				continue
			}
			volume = true
			if a.Input != nil {
				in := *a.Input
				in.Default = ""
				a.Input = &in
			}
		}
		// lazylazy reserva enter para seguir un link.
		if a.Key == "enter" {
			a.Key = "g"
		}
		out = append(out, a)
	}
	v.Actions = out
	return v
}

// panelOptions son el idioma y, de config.json del panel, unidades y tarjetas.
func (a *app) panelOptions() view.Options {
	o := view.Options{Lang: a.env.Lang, Units: "c"}
	dir, err := os.UserConfigDir()
	if err != nil {
		return o
	}
	data, err := os.ReadFile(filepath.Join(dir, "sysoscope", "config.json"))
	if err != nil {
		return o
	}
	var c struct {
		Units string   `json:"units"`
		Cards []string `json:"cards"`
	}
	if json.Unmarshal(data, &c) != nil {
		return o
	}
	if c.Units == "f" {
		o.Units = "f"
	}
	o.Cards = c.Cards
	return o
}

// lazyLine es una línea del stream: la vista y las secciones (para los badges).
type lazyLine struct {
	*view.View
	Sections []view.Section `json:"sections"`
}

// lazyStream manda una vista por tick hasta que se cierra stdin. No escribe
// latest.json: ese es del stream del panel.
func (a *app) lazyStream(id string, o view.Options) int {
	eng := a.newEngine(a.env, engine.Options{Interval: 2 * time.Second, Stream: true, Focus: engineFocus(id)})
	defer eng.Close()
	o.Recent = eng.Recent
	done := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(a.stdin)
		for sc.Scan() {
		}
		close(done)
	}()
	out := bufio.NewWriter(a.stdout)
	enc := json.NewEncoder(out)
	// Una lectura de calentamiento, como fresh: el uso de CPU y las tasas se
	// calculan por diferencia y la primera vista saldría sin cabecera.
	eng.Tick()
	a.sleep(250 * time.Millisecond)
	for {
		snap := eng.Tick()
		if v, err := view.Build(id, snap, o); err == nil {
			enc.Encode(lazyLine{View: forLazylazy(v), Sections: view.Sections(snap, o)})
			out.Flush()
		}
		select {
		case <-a.ctx.Done():
			return 0
		case <-done:
			return 0
		case <-time.After(eng.Interval()):
		}
	}
}

// firmwareCheck busca actualizaciones de firmware ahora e imprime el resultado.
func (a *app) firmwareCheck() int {
	r := &collect.Report{Lang: a.env.Lang}
	fw := system.Firmware(a.env, r)
	w := bufio.NewWriter(a.stdout)
	defer w.Flush()
	for _, u := range r.List {
		w.WriteString(i18n.F(a.env.Lang, "not available: %s", u.Message) + "\n")
	}
	n := 0
	if fw != nil {
		for _, d := range fw.Devices {
			if d.Available != "" {
				n++
				w.WriteString(d.Name + ": " + d.Current + " → " + d.Available + "\n")
			}
		}
	}
	if n == 0 && len(r.List) == 0 {
		w.WriteString(i18n.T(a.env.Lang, "No firmware updates") + "\n")
	}
	return 0
}

// updatesCheck busca actualizaciones de los repos oficiales ahora (sale a
// internet; nunca el AUR), guarda el resultado para el panel e imprime la
// lista.
func (a *app) updatesCheck() int {
	l := a.env.Lang
	u := maint.CheckUpdates(a.env, maint.CheckupDB(os.Getenv, os.Getuid()))
	engine.SaveUpdates(a.env.Cache, u)
	w := bufio.NewWriter(a.stdout)
	defer w.Flush()
	switch {
	case u.Error != "":
		w.WriteString(i18n.F(l, "Could not check for updates: %s", u.Error) + "\n")
	case len(u.Repo) == 0:
		w.WriteString(i18n.T(l, "No updates available") + "\n")
	default:
		for _, p := range u.Repo {
			repo := p.Repo
			if repo == "" {
				repo = "—"
			}
			fmt.Fprintf(w, "%s %s → %s (%s)\n", p.Name, p.Old, p.New, repo)
		}
	}
	w.WriteString(i18n.T(l, "AUR packages are not checked (that would send their names to aur.archlinux.org)") + "\n")
	return 0
}
