package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/audio"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/bluetooth"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/engine"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/ondisk"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/power"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/view"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/wifi"
)

func (a *app) stream(args []string) int {
	fs := flag.NewFlagSet("stream", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	interval := fs.Duration("interval", 2*time.Second, "time between snapshots (1s–60s)")
	vf := addViewFlags(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	a.setLang(*vf.lang)
	opts := a.viewOptions(vf)
	focus := "" // la vista abierta en el panel
	eng := a.newEngine(a.env, engine.Options{Interval: *interval, Stream: true})
	defer eng.Close()
	opts.Recent = eng.Recent

	cmds := make(chan string)
	go func() {
		sc := bufio.NewScanner(a.stdin)
		for sc.Scan() {
			cmds <- strings.TrimSpace(sc.Text())
		}
		close(cmds)
	}()
	// Las acciones de apps corren aparte (forzar el cierre espera hasta 3 s);
	// cada resultado sale en una línea siguiente, en orden de llegada. Al
	// salir se esperan (hasta actionWait): un SIGKILL no puede quedar a medias.
	results := make(chan actionResult)
	var pending []actionResult
	var running sync.WaitGroup
	exited := make(chan struct{})
	defer func() {
		close(exited) // nadie lee más results: los que terminen no se traban
		finished := make(chan struct{})
		go func() { running.Wait(); close(finished) }()
		select {
		case <-finished:
		case <-time.After(actionWait):
		}
	}()

	out := bufio.NewWriter(a.stdout)
	latest := filepath.Join(a.env.State, "latest.json")
	// last y lastData son la última muestra: pausado, los resultados salen
	// sobre ella en vez de tomar otra.
	var last *model.Snapshot
	var lastData []byte
	// searches es la búsqueda por contenido que espera su línea.
	var searches searchBox
	// write manda una línea con la última muestra y, si hay, un resultado.
	write := func() {
		var v *view.View
		if focus != "" {
			v, _ = view.Build(focus, last, opts)
		}
		var res *actionResult
		if len(pending) > 0 {
			res = &pending[0]
			pending = pending[1:]
		}
		srch := searches.take(last, opts)
		out.Write(append(withView(lastData, view.Sections(last, opts), v, res, srch), '\n'))
		out.Flush()
	}
	tick := func() {
		snap := eng.Tick()
		data, err := json.Marshal(snap)
		if err != nil {
			fmt.Fprintf(a.stderr, "sysoscope: %v\n", err)
			return
		}
		last, lastData = snap, data
		write()
		// En disco, sin lo que identifica (ver ondisk); la línea del
		// stream lo lleva todo.
		if data, err = json.Marshal(ondisk.Snapshot(snap, a.env.Home, a.env.User.Name)); err != nil {
			fmt.Fprintf(a.stderr, "sysoscope: %v\n", err)
			return
		}
		engine.WriteAtomic(latest, data)
	}

	paused := false
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return 0
		case <-timer.C:
			if !paused {
				tick()
			}
			timer.Reset(eng.Interval())
		case r := <-results:
			pending = append(pending, r)
			if wifi.Known(r.ID) {
				eng.RefreshWifi() // la acción cambió el Wi-Fi: se relee ya
			}
			if power.Known(r.ID) {
				eng.RefreshPower() // la acción cambió el perfil: se relee ya
			}
			if audio.Known(r.ID) {
				// La acción cambió el audio: se relee ya. Solo
				// audio.default hace que la disponibilidad se vuelva a
				// correr; las demás reusan lo que había en caché.
				eng.RefreshAudio(r.ID == "audio.default")
			}
			if bluetooth.Known(r.ID) {
				eng.RefreshBluetooth() // el resultado real llega después, por bt-wait
			}
			if !paused {
				tick()
				timer.Reset(eng.Interval())
			} else if last == nil {
				tick() // todavía no hay muestra: una sola
			}
			for last != nil && len(pending) > 0 {
				write()
			}
		case line, ok := <-cmds:
			if !ok {
				return 0 // stdin cerrado: el panel se fue
			}
			now := false
			f := strings.Fields(line)
			if len(f) == 0 {
				continue
			}
			switch f[0] {
			case "quit", "exit":
				return 0
			case "pause":
				paused = true
			case "resume":
				paused, now = false, true
			case "refresh":
				eng.RefreshSlow()
				eng.RefreshWifi()
				eng.RefreshMaintenance()
				now = true
			case "focus":
				focus = strings.Join(f[1:], " ")
				if focus != "" && !slices.Contains(view.IDs, focus) {
					fmt.Fprintf(a.stderr, "sysoscope: %s\n", i18n.F(a.env.Lang, "unknown section %q", focus))
					focus = ""
				}
				eng.SetFocus(engineFocus(focus))
				now = true
			case "lang":
				a.setLang(strings.Join(f[1:], ""))
				opts.Lang = a.env.Lang
				eng.SetLang(a.env.Lang)
				now = true
			case "units":
				if u := strings.Join(f[1:], ""); u == "c" || u == "f" {
					opts.Units = u
					now = true
				} else {
					fmt.Fprintf(a.stderr, "sysoscope: %s\n", i18n.T(a.env.Lang, "usage: units c|f"))
				}
			case "cards":
				opts.Cards = splitCards(strings.Join(f[1:], ""))
				now = true
			case "action":
				id, rest, _ := strings.Cut(strings.TrimSpace(strings.TrimPrefix(line, "action")), " ")
				switch {
				case id == "firmware.check":
					eng.CheckFirmware()
				case id == "updates.check":
					eng.CheckUpdates()
				case isAction(id):
					var arg []string
					if rest = strings.TrimSpace(rest); rest != "" && json.Unmarshal([]byte(rest), &arg) != nil {
						fmt.Fprintf(a.stderr, "sysoscope: %s\n", i18n.T(a.env.Lang, "usage: action ID [a JSON array with the row and the input]"))
						continue
					}
					arg = append(arg, "", "")
					running.Add(1)
					go func(env source.Env, id, target, input string) {
						defer running.Done()
						r := runAction(env, id, target, input)
						select {
						case results <- r:
						case <-exited:
						}
					}(a.env, id, arg[0], arg[1])
				default:
					fmt.Fprintf(a.stderr, "sysoscope: %s\n", i18n.F(a.env.Lang, "unknown action %q", id))
				}
			case "search":
				// La consulta es un string JSON; la respuesta sale ya, en una
				// línea extra con la última muestra (sin tomar otra, también
				// en pausa). Sin muestra todavía, sale con la primera.
				q := ""
				if rest := strings.TrimSpace(strings.TrimPrefix(line, "search")); rest != "" && json.Unmarshal([]byte(rest), &q) != nil {
					fmt.Fprintf(a.stderr, "sysoscope: %s\n", i18n.T(a.env.Lang, "usage: search \"QUERY\" (a JSON string)"))
					continue
				}
				searches.ask(q)
				if last != nil {
					write()
				}
			case "interval":
				secs, err := strconv.ParseFloat(strings.Join(f[1:], ""), 64)
				if err != nil || secs <= 0 {
					fmt.Fprintf(a.stderr, "sysoscope: %s\n", i18n.T(a.env.Lang, "usage: interval <seconds>"))
					continue
				}
				eng.SetInterval(time.Duration(secs * float64(time.Second)))
				timer.Reset(eng.Interval())
			default:
				fmt.Fprintf(a.stderr, "sysoscope: %s\n", i18n.F(a.env.Lang, "unknown stream command %q", f[0]))
			}
			if now && !paused {
				tick()
				timer.Reset(eng.Interval())
			}
		}
	}
}
