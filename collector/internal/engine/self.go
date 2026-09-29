package engine

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// selfMeter mide el costo del propio colector: CPU por tick (con la de sus
// hijos) y RSS.
type selfMeter struct {
	last    time.Duration
	samples []float64
	started bool
}

// cpuTime es la CPU del colector más la de sus hijos ya esperados (ss,
// nvidia-smi y los demás comandos): lo que de verdad cuesta una muestra.
func cpuTime() time.Duration {
	var self, kids syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &self) != nil {
		return 0
	}
	t := self.Utime.Nano() + self.Stime.Nano()
	if syscall.Getrusage(syscall.RUSAGE_CHILDREN, &kids) == nil {
		t += kids.Utime.Nano() + kids.Stime.Nano()
	}
	return time.Duration(t)
}

// measure devuelve el tiempo de CPU desde la última medición y el promedio
// móvil de 30 ticks, en milisegundos.
func (m *selfMeter) measure(start time.Duration) (float64, float64) {
	return m.add(float64(cpuTime()-start) / float64(time.Millisecond))
}

// add suma un tick de ms. El primero lee todo el inventario una sola vez: no
// es lo que cuesta una muestra y queda fuera del promedio (mientras sea el
// único, el promedio es él mismo).
func (m *selfMeter) add(ms float64) (float64, float64) {
	if !m.started {
		m.started = true
		return ms, ms
	}
	m.samples = append(m.samples, ms)
	if len(m.samples) > 30 {
		m.samples = m.samples[1:]
	}
	sum := 0.0
	for _, s := range m.samples {
		sum += s
	}
	return ms, sum / float64(len(m.samples))
}

// rss lee VmRSS del propio proceso (siempre del sistema real).
func rss() uint64 {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, l := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(l, "VmRSS:"); ok {
			f := strings.Fields(v)
			if len(f) > 0 {
				n, _ := strconv.ParseUint(f[0], 10, 64)
				return n * 1024
			}
		}
	}
	return 0
}
