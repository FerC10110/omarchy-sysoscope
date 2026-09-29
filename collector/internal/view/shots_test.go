package view

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

// TestWriteShots escribe las vistas de los fixtures para el arnés de capturas
// (tools/shots). Los fixtures son estáticos, así que el historial reciente se
// inventa con una onda suave para que las columnas se vean.
func TestWriteShots(t *testing.T) {
	dir := os.Getenv("SYSOSCOPE_SHOTS")
	if dir == "" {
		t.Skip("SYSOSCOPE_SHOTS not set")
	}
	wave := func(base, amp float64) []float64 {
		out := make([]float64, 60)
		for i := range out {
			out[i] = math.Max(0, base+amp*math.Sin(float64(i)/3)+amp/2*math.Sin(float64(i)*1.7))
		}
		return out
	}
	recent := map[string][]float64{
		"cpu.usage": wave(12, 6), "cpu.temp": wave(38, 3), "mem.used_pct": wave(30, 1),
		"gpu.usage": wave(18, 5), "gpu.temp": wave(52, 2), "net.rx": wave(17000, 9000),
		"net.tx": wave(9000, 4000), "disk.read": wave(200000, 150000), "bat.pct": wave(80, 1),
	}
	for _, fx := range []string{"threadripper", "laptop"} {
		for _, lang := range []string{"en", "es"} {
			s, _ := snap(t, fx)
			s.Lang = lang
			s.Apps = appsSnap().Apps // las capturas muestran la sección con datos
			// …y sin los avisos de apps del fixture, que contradirían esos datos.
			var un []model.Unavailable
			for _, u := range s.Unavailable {
				if !strings.HasPrefix(u.What, "apps.") {
					un = append(un, u)
				}
			}
			s.Unavailable = un
			o := Options{Lang: lang, Recent: func(m string) []float64 { return recent[m] }}
			out := filepath.Join(dir, fx+"-"+lang)
			if err := os.MkdirAll(out, 0o755); err != nil {
				t.Fatal(err)
			}
			write := func(name string, v any) {
				b, err := json.Marshal(v)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(out, name), b, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			for _, id := range IDs {
				v, err := Build(id, s, o)
				if err != nil {
					t.Fatal(err)
				}
				write(id+".json", v)
			}
			write("sections.json", Sections(s, o))
			write("snapshot.json", s)
		}
	}
}
