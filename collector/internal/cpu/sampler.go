package cpu

import (
	"strconv"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/sensors"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

type times struct{ total, idle uint64 }

// Sampler guarda la lectura anterior de /proc/stat para calcular el uso.
// Con SkipFreq no relee las frecuencias (24 archivos de cpufreq cuestan ~2.6
// ms de CPU) y conserva la última lectura.
type Sampler struct {
	SkipFreq bool
	prev     []times // [0] = total, [1:] = por CPU
	hasPrev  bool
}

// Sample completa los campos dinámicos de c. El primer Sample no calcula el
// uso (necesita dos lecturas). sens es la lectura de sensores de la misma
// muestra (puede ser nil).
func (s *Sampler) Sample(env source.Env, c *model.CPU, sens *model.Sensors, r *collect.Report) {
	cur := readStat(env)
	c.UsagePct, c.PerCPUPct = nil, nil
	if s.hasPrev && len(cur) > 0 && len(cur) == len(s.prev) {
		if u, ok := usage(s.prev[0], cur[0]); ok {
			c.UsagePct = &u
			per := make([]float64, 0, len(cur)-1)
			for i := 1; i < len(cur); i++ {
				p, _ := usage(s.prev[i], cur[i])
				per = append(per, p)
			}
			c.PerCPUPct = per
		}
	}
	s.prev, s.hasPrev = cur, len(cur) > 0

	if !s.SkipFreq || c.FreqMHz == nil {
		freqs(env, c, r)
	}
	if la, err := env.FS.Read("/proc/loadavg"); err == nil {
		f := strings.Fields(la)
		for i := 0; i < 3 && i < len(f); i++ {
			c.Load[i], _ = strconv.ParseFloat(f[i], 64)
		}
	}
	if up, err := env.FS.Read("/proc/uptime"); err == nil {
		if f := strings.Fields(up); len(f) > 0 {
			c.UptimeS, _ = strconv.ParseFloat(f[0], 64)
		}
	}
	if p, err := env.FS.Read("/proc/pressure/cpu"); err == nil {
		c.PSI = collect.ParsePSI(p)
	}
	c.TempC, c.TempSensor = nil, ""
	if v, label, ok := sensors.CPUTemp(sens); ok {
		c.TempC, c.TempSensor = &v, label
	} else {
		r.Absent("cpu.temp")
	}
}

func usage(a, b times) (float64, bool) {
	if b.total <= a.total {
		return 0, false
	}
	dt := float64(b.total - a.total)
	di := float64(0)
	if b.idle > a.idle {
		di = float64(b.idle - a.idle)
	}
	u := (dt - di) / dt * 100
	return min(max(u, 0), 100), true
}

// readStat lee las líneas cpu de /proc/stat: user nice system idle iowait
// irq softirq steal (guest ya está incluido en user).
func readStat(env source.Env) []times {
	lines, err := env.FS.Lines("/proc/stat")
	if err != nil {
		return nil
	}
	var out []times
	for _, l := range lines {
		if !strings.HasPrefix(l, "cpu") {
			break
		}
		f := strings.Fields(l)
		var t times
		for i := 1; i < len(f) && i <= 8; i++ {
			v, _ := strconv.ParseUint(f[i], 10, 64)
			t.total += v
			if i == 4 || i == 5 {
				t.idle += v
			}
		}
		out = append(out, t)
	}
	return out
}

func freqs(env source.Env, c *model.CPU, r *collect.Report) {
	c.FreqMHz, c.AvgMHz = nil, nil
	ids := make([]int, 0, len(c.Topology))
	for _, t := range c.Topology {
		ids = append(ids, t.ID)
	}
	if len(ids) == 0 {
		ids = onlineCPUs(env)
	}
	var sum float64
	for _, id := range ids {
		v, err := env.FS.ReadInt(cpuDir(id) + "/cpufreq/scaling_cur_freq")
		if err != nil {
			c.FreqMHz = nil
			r.Absent("cpu.freq")
			return
		}
		f := float64(v) / 1000
		c.FreqMHz = append(c.FreqMHz, f)
		sum += f
	}
	if len(c.FreqMHz) == 0 {
		r.Absent("cpu.freq")
		return
	}
	avg := sum / float64(len(c.FreqMHz))
	c.AvgMHz = &avg
}
