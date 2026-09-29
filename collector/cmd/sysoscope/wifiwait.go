package main

import "github.com/FerC10110/omarchy-sysoscope/collector/internal/wifi"

// wifiWait corre lo que lanza una acción de Wi-Fi desacoplada (los tests la
// reemplazan).
var wifiWait = wifi.Wait

// wifiWaitCmd es `sysoscope wifi-wait ID FILA DATO -- ORDEN [ARGS]`: lo lanza
// una acción de Wi-Fi (conectar, cambiar la banda) en su propia sesión, y
// anota el resultado en actions.log. No imprime nada: nadie lo lee.
func (a *app) wifiWaitCmd(args []string) int {
	if len(args) < 5 || args[3] != "--" {
		return a.fail("usage: sysoscope wifi-wait ID TARGET INPUT -- COMMAND [ARGS]")
	}
	if wifiWait(a.env, args[0], args[1], args[2], args[4:]) != nil {
		return 1
	}
	return 0
}
