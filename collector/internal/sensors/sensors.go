// Package sensors lee /sys/class/hwmon: temperaturas, ventiladores y voltajes
// con nombres legibles, y elige la temperatura real del CPU.
package sensors

import (
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

var inputRe = regexp.MustCompile(`^(temp|fan|in)(\d+)_input$`)

// slowChip es el costo de lectura a partir del cual un chip se lee con
// menos frecuencia (el WMI de las placas ASUS tarda ~200 ms y ~55 ms de CPU).
const slowChip = 5 * time.Millisecond

// Reader lee los chips hwmon recordando cuánto tarda cada uno. Los chips
// lentos (salvo los del CPU) se releen cada SlowEvery, o siempre con Focus
// (la sección Sensores abierta); mientras tanto se repite la última lectura.
type Reader struct {
	Focus     bool
	SlowEvery time.Duration
	cost      map[string]time.Duration
	at        map[string]time.Time
	last      map[string]model.Chip
}

// Read lee todos los chips hwmon una vez (sin estado).
func Read(env source.Env, r *collect.Report) *model.Sensors {
	return (&Reader{}).Read(env, r)
}

// Read lee los chips hwmon, ordenados por número.
func (rd *Reader) Read(env source.Env, r *collect.Report) *model.Sensors {
	if rd.cost == nil {
		rd.cost, rd.at, rd.last = map[string]time.Duration{}, map[string]time.Time{}, map[string]model.Chip{}
	}
	s := &model.Sensors{Chips: []model.Chip{}}
	dirs, err := env.FS.Glob("/sys/class/hwmon/hwmon*")
	if err != nil || len(dirs) == 0 {
		r.Absent("sensors")
		return s
	}
	sort.Slice(dirs, func(i, j int) bool { return hwNum(dirs[i]) < hwNum(dirs[j]) })
	isNet := func(n string) bool { return env.FS.Exists("/sys/class/net/" + n) }
	var now time.Time
	if env.Now != nil {
		now = env.Now()
	}
	for _, d := range dirs {
		id := path.Base(d)
		// una dGPU suspendida (Optimus) se despertaría al leer sus sensores
		if st, _ := env.FS.Read(d + "/device/power/runtime_status"); strings.TrimSpace(st) == "suspended" {
			delete(rd.last, id)
			continue
		}
		name, _ := env.FS.Read(d + "/name")
		name = strings.TrimSpace(name)
		c := model.Chip{ID: id, Name: name, Kind: kindOf(name, isNet)}
		if prev, ok := rd.last[id]; ok && c.Kind != "cpu" && !rd.Focus && rd.cost[id] > slowChip && now.Sub(rd.at[id]) < rd.SlowEvery {
			s.Chips = append(s.Chips, prev)
			continue
		}
		t0 := time.Now()
		readInputs(env, d, &c)
		rd.cost[id], rd.at[id] = time.Since(t0), now
		if len(c.Temps)+len(c.Fans)+len(c.Volts) == 0 {
			delete(rd.last, id)
			continue
		}
		devModel, _ := env.FS.Read(d + "/device/model")
		c.Label = baseLabel(c.Kind, name, strings.TrimSpace(devModel))
		rd.last[id] = c
		s.Chips = append(s.Chips, c)
	}
	numberRepeated(s.Chips)
	return s
}

func hwNum(p string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(path.Base(p), "hwmon"))
	return n
}

type input struct {
	kind string
	n    int
}

func readInputs(env source.Env, dir string, c *model.Chip) {
	names, err := env.FS.Dir(dir)
	if err != nil {
		return
	}
	var ins []input
	for _, f := range names {
		if m := inputRe.FindStringSubmatch(f); m != nil {
			n, _ := strconv.Atoi(m[2])
			ins = append(ins, input{m[1], n})
		}
	}
	sort.Slice(ins, func(i, j int) bool {
		if ins[i].kind != ins[j].kind {
			return ins[i].kind < ins[j].kind
		}
		return ins[i].n < ins[j].n
	})
	for _, in := range ins {
		base := dir + "/" + in.kind + strconv.Itoa(in.n)
		raw, err := env.FS.ReadInt(base + "_input")
		if err != nil {
			continue
		}
		label, err := env.FS.Read(base + "_label")
		label = strings.TrimSpace(label)
		if err != nil || label == "" {
			label = in.kind + strconv.Itoa(in.n)
		}
		rd := model.Reading{Label: label}
		switch in.kind {
		case "temp":
			rd.Value = float64(raw) / 1000
			rd.High = milli(env, base+"_max")
			rd.Crit = milli(env, base+"_crit")
			c.Temps = append(c.Temps, rd)
		case "fan":
			rd.Value = float64(raw)
			c.Fans = append(c.Fans, rd)
		case "in":
			rd.Value = float64(raw) / 1000
			c.Volts = append(c.Volts, rd)
		}
	}
}

// milli lee un límite en milésimas. Un límite sin configurar llega como un
// valor absurdo (nvme: 65261.85 °C, 0xFFFF kelvin); por encima de 1000 °C no
// es un límite.
func milli(env source.Env, p string) *float64 {
	v, err := env.FS.ReadInt(p)
	if err != nil || v <= 0 || v >= 1000000 {
		return nil
	}
	f := float64(v) / 1000
	return &f
}

// numberRepeated distingue chips con la misma etiqueta: los CPU como
// "CPU (die N)", el resto como "RAM 1", "RAM 2"… (la RAM siempre numerada).
func numberRepeated(chips []model.Chip) {
	count := map[string]int{}
	for _, c := range chips {
		count[c.Label]++
	}
	seen := map[string]int{}
	for i := range chips {
		c := &chips[i]
		l := c.Label
		if count[l] < 2 && c.Kind != "ram" {
			continue
		}
		seen[l]++
		if c.Kind == "cpu" {
			c.Label = l + " (die " + strconv.Itoa(seen[l]-1) + ")"
		} else {
			c.Label = l + " " + strconv.Itoa(seen[l])
		}
	}
}

// CPUTemp elige la temperatura del CPU sin offset. Orden de preferencia: el
// mayor Tdie; el mayor Tccd*; "Package id 0" (coretemp); el temp1 del chip de
// CPU (ARM); Tctl solo si no hay nada más (en Threadripper tiene +27 °C).
func CPUTemp(s *model.Sensors) (float64, string, bool) {
	if s == nil {
		return 0, "", false
	}
	pick := func(match func(string) bool) (float64, string, bool) {
		best, label, ok := 0.0, "", false
		for _, c := range s.Chips {
			if c.Kind != "cpu" {
				continue
			}
			for _, t := range c.Temps {
				if match(t.Label) && (!ok || t.Value > best) {
					best, label, ok = t.Value, c.Name+" "+t.Label, true
				}
			}
		}
		return best, label, ok
	}
	for _, m := range []func(string) bool{
		func(l string) bool { return l == "Tdie" },
		func(l string) bool { return strings.HasPrefix(l, "Tccd") },
		func(l string) bool { return l == "Package id 0" },
		func(l string) bool { return l == "temp1" },
		func(l string) bool { return l == "Tctl" },
	} {
		if v, l, ok := pick(m); ok {
			return v, l, true
		}
	}
	return 0, "", false
}
