package collect

import (
	"strconv"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

// ParsePSI interpreta /proc/pressure/*: "some avg10=0.00 avg60=0.01 …" y
// opcionalmente "full …". Devuelve nil si no hay línea "some".
func ParsePSI(text string) *model.PSI {
	var p model.PSI
	some := false
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		a10, ok10 := psiField(f, "avg10=")
		a60, ok60 := psiField(f, "avg60=")
		if !ok10 || !ok60 {
			continue
		}
		switch f[0] {
		case "some":
			p.Some10, p.Some60, some = a10, a60, true
		case "full":
			p.Full10, p.Full60 = &a10, &a60
		}
	}
	if !some {
		return nil
	}
	return &p
}

func psiField(f []string, prefix string) (float64, bool) {
	for _, s := range f {
		if v, ok := strings.CutPrefix(s, prefix); ok {
			n, err := strconv.ParseFloat(v, 64)
			return n, err == nil
		}
	}
	return 0, false
}

var placeholders = map[string]bool{
	"to be filled by o.e.m.": true, "default string": true, "system product name": true,
	"system version": true, "system manufacturer": true, "sku": true, "not specified": true,
	"none": true, "0123456789": true, "o.e.m.": true, "not applicable": true,
}

// Placeholder dice si s es un texto de relleno de DMI (o vacío).
func Placeholder(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "" || placeholders[s]
}

// Clean recorta s y devuelve "" si es un texto de relleno.
func Clean(s string) string {
	if Placeholder(s) {
		return ""
	}
	return strings.TrimSpace(s)
}
