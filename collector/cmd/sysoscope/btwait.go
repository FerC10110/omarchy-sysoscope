package main

import "github.com/FerC10110/omarchy-sysoscope/collector/internal/bluetooth"

// btWait corre lo que lanza una acción de Bluetooth desacoplada (los tests
// la reemplazan).
var btWait = bluetooth.Wait

// btWaitCmd es `sysoscope bt-wait ID FILA -- ORDEN [ARGS]`: lo lanza una
// acción de Bluetooth (prender, apagar, conectar, desconectar, olvidar) en
// su propia sesión, y anota el resultado real en actions.log. No imprime
// nada: nadie lo lee.
func (a *app) btWaitCmd(args []string) int {
	if len(args) < 4 || args[2] != "--" {
		return a.fail("usage: sysoscope bt-wait ID TARGET -- COMMAND [ARGS]")
	}
	if btWait(a.env, args[0], args[1], args[3:]) != nil {
		return 1
	}
	return 0
}
