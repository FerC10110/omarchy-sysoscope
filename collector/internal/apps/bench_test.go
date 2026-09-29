package apps

import (
	"os"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// BenchmarkAppsPassLive mide la pasada de apps sobre el /proc real, sin y con
// /proc/<pid>/io: la diferencia es el costo del disco por app. Solo lee
// (/proc; sin comandos: ss y nvidia-smi van por un FakeRunner vacío) y solo
// corre con SYSOSCOPE_LIVE_BENCH=1: go test nunca toca el sistema en vivo.
func BenchmarkAppsPassLive(b *testing.B) {
	if os.Getenv("SYSOSCOPE_LIVE_BENCH") != "1" {
		b.Skip("set SYSOSCOPE_LIVE_BENCH=1 to read the real /proc")
	}
	env := source.Env{FS: source.FS{}, Run: &source.FakeRunner{Dir: b.TempDir()},
		Hypr: &source.FakeHypr{Dir: b.TempDir()}, Now: time.Now, Lang: "en"}
	// Tres casos: sin disco; con disco y muestras seguidas (casi ningún
	// proceso cambia de ticks entre dos: casi todos se saltean, es el piso); y
	// con disco y una cuarta parte de los procesos con ticks nuevos entre
	// muestras (lo medido en esta máquina en 4 s: 20-26 %), que es lo real.
	for _, c := range []struct {
		name   string
		disk   bool
		active int // 1 de cada active procesos "cambió"; 0 = ninguno
	}{{"disk=false", false, 0}, {"disk=true/idle", true, 0}, {"disk=true/mix25", true, 4}} {
		b.Run(c.name, func(b *testing.B) {
			s := &Sampler{UID: uint32(os.Getuid()), Disk: c.disk}
			s.Sample(env, nil, 1, false, &collect.Report{Lang: "en"})
			reads := 0
			old := readIO
			readIO = func(fs source.FS, pid int) (string, error) { reads++; return old(fs, pid) }
			defer func() { readIO = old }()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s.Sample(env, nil, 1, false, &collect.Report{Lang: "en"})
				if c.active > 0 {
					b.StopTimer()
					n := 0
					for k := range s.cpuLast {
						if n++; n%c.active == 0 {
							s.cpuLast[k]--
						}
					}
					b.StartTimer()
				}
			}
			b.ReportMetric(float64(reads)/float64(b.N), "io-reads/op")
			if c.disk {
				b.ReportMetric(float64(len(s.cpuLast))-float64(reads)/float64(b.N), "skipped/op")
			}
		})
	}
}
