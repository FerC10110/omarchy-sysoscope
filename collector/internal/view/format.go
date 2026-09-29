package view

import (
	"fmt"
	"math"
	"strings"
)

// Los números usan punto decimal en los dos idiomas, como free, sensors y btop.

// Bytes en unidades IEC con un decimal: 1536 → "1.5 KiB".
func Bytes(b uint64) string {
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	v := float64(b) / 1024
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

// Rate en unidades SI por segundo: 2100000 → "2.1 MB/s".
func Rate(bps float64) string {
	if bps < 1000 {
		return fmt.Sprintf("%.0f B/s", bps)
	}
	units := []string{"kB/s", "MB/s", "GB/s", "TB/s"}
	v := bps / 1000
	i := 0
	for v >= 1000 && i < len(units)-1 {
		v /= 1000
		i++
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

// Pct sin decimales, salvo por debajo de 1: 0.4 → "0.4%".
func Pct(v float64) string {
	if v > 0 && v < 1 {
		return fmt.Sprintf("%.1f%%", v)
	}
	return fmt.Sprintf("%.0f%%", v)
}

// Temp en °C o °F ("f"), sin decimales.
func Temp(c float64, units string) string {
	if units == "f" {
		return fmt.Sprintf("%.0f °F", c*9/5+32)
	}
	return fmt.Sprintf("%.0f °C", c)
}

// MHz: desde 1000 en GHz con dos decimales.
func MHz(v float64) string {
	if v >= 1000 {
		return fmt.Sprintf("%.2f GHz", v/1000)
	}
	return fmt.Sprintf("%.0f MHz", v)
}

// Duration en las dos unidades más grandes: "3 d 4 h", "1 h 2 min", "42 s".
// Las abreviaturas son iguales en inglés y en español.
func Duration(secs float64, lang string) string {
	s := int64(math.Max(secs, 0))
	d, h, m := s/86400, s%86400/3600, s%3600/60
	switch {
	case d > 0:
		return fmt.Sprintf("%d d %d h", d, h)
	case h > 0:
		return fmt.Sprintf("%d h %d min", h, m)
	case m > 0:
		return fmt.Sprintf("%d min %d s", m, s%60)
	}
	return fmt.Sprintf("%d s", s)
}

func Watts(w float64) string { return fmt.Sprintf("%.1f W", w) }

// Level: "crit" desde crit, "warn" desde warn, si no "ok".
func Level(v, warn, crit float64) string {
	switch {
	case v >= crit:
		return "crit"
	case v >= warn:
		return "warn"
	}
	return "ok"
}

// Split separa un valor formateado en número y unidad: "7%" → "7", "%";
// "34 °C" → "34", "°C"; "1.1 TiB free" → "1.1", "TiB free". Sin % ni espacio,
// todo es número.
func Split(s string) (string, string) {
	if strings.HasSuffix(s, "%") {
		return strings.TrimSuffix(s, "%"), "%"
	}
	if i := strings.IndexByte(s, ' '); i > 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}
