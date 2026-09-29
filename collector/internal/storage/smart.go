package storage

import (
	"math"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

const (
	udisks   = "org.freedesktop.UDisks2"
	ifDrive  = "org.freedesktop.UDisks2.Drive"
	ifAta    = "org.freedesktop.UDisks2.Drive.Ata"
	ifNVMe   = "org.freedesktop.UDisks2.NVMe.Controller"
	kelvin0C = 273.15
)

// Smart lee SMART de udisks2 (sin root) y lo asocia a cada disco por número
// de serie, o por modelo si no hay serie. Va en el ritmo lento (60 s).
func Smart(env source.Env, disks []model.Disk, r *collect.Report) {
	objs, err := env.Bus.Objects(true, udisks, "/org/freedesktop/UDisks2")
	if err != nil {
		r.Missing("storage.smart", "udisks2")
		return
	}
	for i := range disks {
		d := &disks[i]
		d.Smart = nil
		for p, ifs := range objs {
			drv, ok := ifs[ifDrive]
			if !ok || !matches(drv, d) {
				continue
			}
			if nv, ok := ifs[ifNVMe]; ok {
				d.Smart = nvme(env, p, nv)
			} else if ata, ok := ifs[ifAta]; ok && boolOf(ata["SmartEnabled"]) {
				d.Smart = ataSmart(ata)
			}
			break
		}
	}
}

func matches(drv map[string]any, d *model.Disk) bool {
	serial, _ := drv["Serial"].(string)
	if serial != "" && d.Serial != "" {
		return serial == d.Serial
	}
	m, _ := drv["Model"].(string)
	return m != "" && m == d.Model
}

func nvme(env source.Env, path string, nv map[string]any) *model.Smart {
	s := &model.Smart{Kind: "nvme", Warnings: []string{}}
	if w, ok := nv["SmartCriticalWarning"].([]any); ok {
		for _, x := range w {
			if str, ok := x.(string); ok {
				s.Warnings = append(s.Warnings, str)
			}
		}
	}
	s.Failing = len(s.Warnings) > 0
	s.PowerOnHours = intp(nv["SmartPowerOnHours"])
	s.TempC = celsius(nv["SmartTemperature"])
	s.Updated = updated(nv["SmartUpdated"])
	body, err := env.Bus.Call(true, udisks, path, ifNVMe+".SmartGetAttributes", map[string]any{})
	if err == nil && len(body) > 0 {
		if attrs, ok := body[0].(map[string]any); ok {
			s.PercentUsed = intp(attrs["percent_used"])
			s.MediaErrors = intp(attrs["media_errors"])
		}
	}
	if len(s.Warnings) == 0 {
		s.Warnings = nil
	}
	return s
}

func ataSmart(a map[string]any) *model.Smart {
	s := &model.Smart{Kind: "ata", Failing: boolOf(a["SmartFailing"])}
	if secs, ok := num(a["SmartPowerOnSeconds"]); ok && secs > 0 {
		h := int(secs / 3600)
		s.PowerOnHours = &h
	}
	s.TempC = celsius(a["SmartTemperature"])
	s.BadSectors = intp(a["SmartNumBadSectors"])
	s.Updated = updated(a["SmartUpdated"])
	return s
}

// num convierte cualquier número de D-Bus (o de JSON) a float64.
func num(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int16:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint8:
		return float64(x), true
	case uint16:
		return float64(x), true
	case uint32:
		return float64(x), true
	case uint64:
		return float64(x), true
	}
	return 0, false
}

func intp(v any) *int {
	f, ok := num(v)
	if !ok {
		return nil
	}
	n := int(f)
	return &n
}

// celsius convierte kelvin a °C con dos decimales. 0 K significa "sin dato".
func celsius(v any) *float64 {
	k, ok := num(v)
	if !ok || k <= 0 {
		return nil
	}
	c := math.Round((k-kelvin0C)*100) / 100
	return &c
}

func boolOf(v any) bool {
	b, _ := v.(bool)
	return b
}

func updated(v any) time.Time {
	if f, ok := num(v); ok && f > 0 {
		return time.Unix(int64(f), 0).UTC()
	}
	return time.Time{}
}
