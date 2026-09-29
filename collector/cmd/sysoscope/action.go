package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/apps"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/audio"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/bluetooth"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/power"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/wifi"
)

// appsRun, wifiRun, powerRun, audioRun y btRun corren una acción de
// Aplicaciones, de Wi-Fi, de Energía, de audio o de Bluetooth (los tests
// las reemplazan).
var (
	appsRun  = apps.Run
	wifiRun  = wifi.Run
	powerRun = power.Run
	audioRun = audio.Run
	btRun    = bluetooth.Run
)

// actionWait es lo máximo que el stream espera, al salir, a las acciones en
// curso (forzar el cierre espera 3 s antes del SIGKILL).
var actionWait = 5 * time.Second

func isAppsAction(id string) bool {
	return strings.HasPrefix(id, "window.") || strings.HasPrefix(id, "app.")
}

// isAction: las acciones que corre el colector (firmware.check y updates.check van aparte).
func isAction(id string) bool {
	return isAppsAction(id) || wifi.Known(id) || power.Known(id) || audio.Known(id) || bluetooth.Known(id)
}

// isSection: las acciones de toda una sección, sin fila (`ID [DATO]`).
func isSection(id string) bool {
	return wifi.IsSection(id) || power.Known(id) || bluetooth.IsSection(id)
}

// actionResult es lo que el stream manda en la línea que sigue a una acción.
type actionResult struct {
	ID   string `json:"id"`
	OK   bool   `json:"ok"`
	Text string `json:"text"`
}

// runAction recibe el Env por valor: el stream la corre en otra goroutine
// mientras su bucle puede cambiar a.env.Lang.
func runAction(env source.Env, id, target, input string) actionResult {
	run := appsRun
	switch {
	case wifi.Known(id):
		run = wifiRun
	case power.Known(id):
		run = powerRun
	case audio.Known(id):
		run = audioRun
	case bluetooth.Known(id):
		run = btRun
	}
	text, err := run(env, id, target, input)
	if err != nil {
		return actionResult{ID: id, Text: err.Error()}
	}
	return actionResult{ID: id, OK: true, Text: text}
}

// actionCmd es `sysoscope [lazylazy] action ID [FILA] [DATO]`: el resultado
// por stdout, o el error solo (lazylazy muestra su última línea) y salida 1.
// Las de toda una sección (wifi.radio, wifi.rescan, wifi.band, power.*,
// bt.power, bt.pair) no llevan fila: `ID [DATO]`, como las manda lazylazy.
func (a *app) actionCmd(args []string) int {
	if len(args) == 0 || !isAction(args[0]) {
		return a.fail("usage: sysoscope action ID [ROW] [INPUT]")
	}
	at := func(i int) string {
		if i < len(args) {
			return args[i]
		}
		return ""
	}
	target, input := at(1), at(2)
	if isSection(args[0]) {
		target, input = "", at(1)
	}
	r := runAction(a.env, args[0], target, input)
	if !r.OK {
		fmt.Fprintln(a.stderr, r.Text)
		return 1
	}
	fmt.Fprintln(a.stdout, r.Text)
	return 0
}
