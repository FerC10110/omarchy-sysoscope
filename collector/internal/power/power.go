// Package power lee baterías (con salud, ciclos y periféricos), si hay
// corriente, el perfil de energía (power-profiles-daemon) y la potencia RAPL.
package power

import (
	"errors"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

const (
	ppd      = "net.hadess.PowerProfiles"
	ppdPath  = "/net/hadess/PowerProfiles"
	supplies = "/sys/class/power_supply/"
)

// Sampler guarda la lectura anterior de RAPL (energía acumulada) y el perfil.
type Sampler struct {
	prev   map[string]int64
	at     time.Time
	profUn []model.Unavailable
}

// Sample actualiza baterías y RAPL en cada muestra, y con slow el perfil, los
// perfiles guardados por Omarchy y si está su script.
func (s *Sampler) Sample(env source.Env, p *model.Power, slow bool, r *collect.Report) {
	batteries(env, p)
	s.rapl(env, p, r)
	if slow {
		pr := &collect.Report{Lang: r.Lang}
		profile(env, p, pr)
		s.profUn = pr.List
	}
	r.Keep(s.profUn)
}

func batteries(env source.Env, p *model.Power) {
	p.Batteries, p.OnAC = nil, nil
	names, _ := env.FS.Dir(supplies)
	for _, n := range names {
		d := supplies + n
		typ, _ := env.FS.Read(d + "/type")
		switch strings.TrimSpace(typ) {
		case "Mains":
			if on, err := env.FS.ReadInt(d + "/online"); err == nil {
				v := on == 1 || (p.OnAC != nil && *p.OnAC)
				p.OnAC = &v
			}
		case "Battery":
			if b, ok := battery(env, n, d); ok {
				p.Batteries = append(p.Batteries, b)
			}
		}
	}
}

func battery(env source.Env, name, d string) (model.Battery, bool) {
	pct, err := env.FS.ReadFloat(d + "/capacity")
	if err != nil {
		return model.Battery{}, false
	}
	b := model.Battery{Name: name, Pct: pct}
	scope, _ := env.FS.Read(d + "/scope")
	b.Peripheral = strings.TrimSpace(scope) == "Device"
	b.Status, _ = env.FS.Read(d + "/status")
	if b.Status == "" {
		b.Status = "Unknown"
	}
	b.Model, _ = env.FS.Read(d + "/model_name")
	b.Manufacturer, _ = env.FS.Read(d + "/manufacturer")
	b.Model, b.Manufacturer = strings.TrimSpace(b.Model), strings.TrimSpace(b.Manufacturer)

	// energía en µWh, o carga en µAh (se multiplica por el voltaje de diseño)
	now, okNow := microF(env, d+"/energy_now")
	full, okFull := microF(env, d+"/energy_full")
	design, okDesign := microF(env, d+"/energy_full_design")
	if !okFull {
		volt, okV := microF(env, d+"/voltage_min_design")
		if !okV {
			volt, okV = microF(env, d+"/voltage_now")
		}
		cn, ok1 := microF(env, d+"/charge_now")
		cf, ok2 := microF(env, d+"/charge_full")
		cd, ok3 := microF(env, d+"/charge_full_design")
		if okV {
			now, okNow = cn*volt, ok1
			full, okFull = cf*volt, ok2
			design, okDesign = cd*volt, ok3
		}
	}
	if okFull && full > 0 {
		b.FullWh = &full
	}
	if okDesign && design > 0 {
		b.DesignWh = &design
		if okFull {
			h := full / design * 100
			b.HealthPct = &h
		}
	}
	if pw, ok := microF(env, d+"/power_now"); ok && pw > 0 {
		b.PowerW = &pw
	} else if cur, ok := microF(env, d+"/current_now"); ok && cur > 0 {
		if v, ok := microF(env, d+"/voltage_now"); ok {
			w := cur * v
			b.PowerW = &w
		}
	}
	if b.PowerW != nil && okNow {
		switch b.Status {
		case "Discharging":
			t := int(now / *b.PowerW * 3600)
			b.TimeLeftS = &t
		case "Charging":
			if okFull && full > now {
				t := int((full - now) / *b.PowerW * 3600)
				b.TimeLeftS = &t
			}
		}
	}
	if c, err := env.FS.ReadInt(d + "/cycle_count"); err == nil && c > 0 {
		v := int(c)
		b.Cycles = &v
	}
	if l, err := env.FS.ReadInt(d + "/charge_control_end_threshold"); err == nil && l > 0 && l <= 100 {
		v := int(l)
		b.ChargeLimitPct = &v
	}
	return b, true
}

// microF lee un valor en micro-unidades (µWh, µW, µA, µV) y lo pasa a la unidad.
func microF(env source.Env, p string) (float64, bool) {
	v, err := env.FS.ReadInt(p)
	if err != nil {
		return 0, false
	}
	return float64(v) / 1e6, true
}

func (s *Sampler) rapl(env source.Env, p *model.Power, r *collect.Report) {
	now := env.Now()
	dt := now.Sub(s.at).Seconds()
	cur := map[string]int64{}
	p.RAPL = nil
	doms, _ := env.FS.Glob("/sys/class/powercap/intel-rapl:*")
	for _, d := range doms {
		e, err := env.FS.ReadInt(d + "/energy_uj")
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				r.Perm("power.rapl", i18n.T(r.Lang, "locked by the kernel; can be unlocked from Settings (stage E)"))
			}
			continue
		}
		cur[d] = e
		prev, had := s.prev[d]
		if !had || dt <= 0 {
			continue
		}
		mx, _ := env.FS.ReadInt(d + "/max_energy_range_uj")
		name, _ := env.FS.Read(d + "/name")
		if name == "" {
			name = path.Base(d)
		}
		p.RAPL = append(p.RAPL, model.PowerDomain{Name: strings.TrimSpace(name), Watts: watts(prev, e, mx, dt)})
	}
	s.prev, s.at = cur, now
}

// watts convierte dos lecturas de energía (µJ) en W, con vuelta del contador.
func watts(prev, cur, max int64, dt float64) float64 {
	d := cur - prev
	if d < 0 {
		d += max
	}
	return float64(d) / 1e6 / dt
}

func profile(env source.Env, p *model.Power, r *collect.Report) {
	v, err := env.Bus.Property(true, ppd, ppdPath, ppd, "ActiveProfile")
	if err != nil {
		p.Profile, p.Profiles = "", nil
		p.SavedAC, p.SavedBattery, p.SetScript = "", "", false
		r.Missing("power.profile", "power-profiles-daemon")
		return
	}
	p.Profile, _ = v.(string)
	p.Profiles = nil
	if list, err := env.Bus.Property(true, ppd, ppdPath, ppd, "Profiles"); err == nil {
		items, _ := list.([]any)
		for _, it := range items {
			if m, ok := it.(map[string]any); ok {
				if name, ok := m["Profile"].(string); ok {
					p.Profiles = append(p.Profiles, name)
				}
			}
		}
	}
	p.SavedAC, p.SavedBattery = saved(env, "ac"), saved(env, "battery")
	p.SetScript = hasCommand(env, setScript)
}
