// Package actlog anota cada acción en <State>/actions.log: hora (UTC), id,
// destino, dato y resultado. Lo usan las acciones de Aplicaciones, Energía,
// Wi-Fi, audio y Bluetooth (también los procesos desacoplados de wifi-wait
// y bt-wait). Nunca recibe una contraseña, y no anota lo que identifica
// (ver private).
package actlog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// sectionInput son las acciones de Wi-Fi, Bluetooth y audio cuyo dato se
// anota: una banda, on/off o un volumen, que no dicen nada del usuario.
var sectionInput = map[string]bool{"wifi.radio": true, "wifi.band": true, "bt.power": true, "audio.volume": true}

// private dice qué partes de la línea se callan. En Wi-Fi, Bluetooth y
// audio, la fila (red, BSSID, dirección, nombre de pactl, que lleva la
// serie USB o la MAC), el dato (salvo sectionInput) y el texto del
// resultado (nombra la red o el dispositivo). En las ventanas, el texto
// (lleva el título o la ruta de la captura): su fila es una dirección.
func private(id string) (target, input, text bool) {
	switch {
	case strings.HasPrefix(id, "wifi."), strings.HasPrefix(id, "bt."), strings.HasPrefix(id, "audio."):
		return true, !sectionInput[id], true
	case strings.HasPrefix(id, "window."):
		return false, false, true
	}
	return false, false, false
}

func Write(env source.Env, id, target, input, out string, err error) {
	hideTarget, hideInput, hideText := private(id)
	if hideTarget {
		target = ""
	}
	if hideInput {
		input = ""
	}
	res := "ok: " + out
	if err != nil {
		res = "error: " + err.Error()
	}
	if hideText {
		res, _, _ = strings.Cut(res, ":")
	}
	if os.MkdirAll(env.State, 0o755) != nil {
		return
	}
	f, ferr := os.OpenFile(filepath.Join(env.State, "actions.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if ferr != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s %q %q %s\n", env.Now().UTC().Format(time.RFC3339), id, target, input, res)
}
