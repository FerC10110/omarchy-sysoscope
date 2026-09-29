package main

import (
	"errors"
	"flag"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/wifi"
)

// wifiHidden pide el nombre de una red oculta y la conecta (los tests la
// reemplazan).
var wifiHidden = wifi.ConnectHidden

// wifiHiddenCmd es `sysoscope wifi-hidden [--hold] [--lang en|es] PLACA
// BSSID`: lo corre wifi.connect_hidden en la terminal (lazylazy en pantalla
// completa) o, desde el panel, en una ventana con omarchy-launch-tui y
// --hold, para que el resultado quede a la vista hasta Enter. Sale con 0
// (conectada), 1 (no se conectó), 2 (uso) o 3 (cancelada): quien lo corre
// en su terminal distingue así qué anotar.
func (a *app) wifiHiddenCmd(args []string) int {
	fs := flag.NewFlagSet("wifi-hidden", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	hold := fs.Bool("hold", false, "wait for Enter before closing")
	lang := fs.String("lang", "", "en or es")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		return a.fail("usage: sysoscope wifi-hidden [--hold] [--lang en|es] IFACE BSSID")
	}
	a.setLang(*lang)
	err := wifiHidden(a.ctx, a.env, fs.Arg(0), fs.Arg(1), a.stdin, a.stdout, *hold)
	var ce *wifi.CancelError
	switch {
	case errors.As(err, &ce):
		return wifi.HiddenExitCancelled
	case err != nil:
		return wifi.HiddenExitFailed
	}
	return 0
}
