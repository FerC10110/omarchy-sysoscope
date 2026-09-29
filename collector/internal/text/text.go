// Package text limpia texto que viene de datos del sistema (nombres de
// dispositivos, EDID, product de USB…) antes de mostrarlo.
package text

import (
	"regexp"
	"strings"
	"unicode"
)

// csiSeq es una secuencia de escape ANSI/CSI completa: ESC '[', los bytes de
// parámetro (dígitos, ';', '?', …), los intermedios si hay, y el byte final
// que la cierra (una letra: 'm' de color, 'H' de posición, etc.) — no solo
// el ESC, que dejaría el resto ("31m") como basura visible.
var csiSeq = regexp.MustCompile("\x1b\\[[0-?]*[ -/]*[@-~]")

// Clean saca las secuencias ANSI/CSI y los caracteres de control y de
// formato Unicode (Cf, como el marcador de dirección RTL U+202E) de un
// texto que viene de datos del sistema, antes de mostrarlo.
func Clean(s string) string {
	s = csiSeq.ReplaceAllString(s, "")
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
}
