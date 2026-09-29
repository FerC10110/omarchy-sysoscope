package mem

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// Modules completa slots, capacidad, ECC y módulos con `inxi -m -c 0`
// (estático, sin root). Si falta inxi, lo registra como no disponible.
func Modules(env source.Env, r *collect.Report, m *model.Memory) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := env.Run.Run(ctx, "inxi", "-m", "-c", "0")
	if err != nil {
		r.FromErr("memory.modules", "inxi", err)
		return
	}
	p := parseInxi(string(out))
	m.Slots, m.CapacityB, m.ECC, m.Modules = p.Slots, p.CapacityB, p.ECC, p.Modules
}

var (
	deviceRe   = regexp.MustCompile(`Device-\d+:\s+(.*?)\s+type:\s+(.*)$`)
	sizeRe     = regexp.MustCompile(`size:\s+([\d.]+)\s+([KMGT]iB)`)
	specRe     = regexp.MustCompile(`spec:\s+(\d+)\s+MT/s`)
	actualRe   = regexp.MustCompile(`actual:\s+(\d+)\s+MT/s`)
	speedRe    = regexp.MustCompile(`speed:\s+(\d+)\s+MT/s`)
	slotsRe    = regexp.MustCompile(`slots:\s+(\d+)`)
	capacityRe = regexp.MustCompile(`capacity:\s+([\d.]+)\s+([KMGT]iB)`)
	eccRe      = regexp.MustCompile(`EC:\s+(\S+)`)
)

// parseInxi interpreta la salida de `inxi -m`. El slot es todo lo que está
// antes de " type:" (los nombres llevan espacios). ActualMTs va crudo: en
// DDR, DMI suele informar la mitad (1067 con spec 2133) y eso lo interpreta
// el Doctor, no el colector.
func parseInxi(text string) model.Memory {
	var m model.Memory
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "Array-") {
			if s := slotsRe.FindStringSubmatch(l); s != nil {
				n, _ := strconv.Atoi(s[1])
				m.Slots += n
			}
			if c := capacityRe.FindStringSubmatch(l); c != nil {
				m.CapacityB += bytesOf(c[1], c[2])
			}
			if e := eccRe.FindStringSubmatch(l); e != nil {
				m.ECC = e[1]
			}
			continue
		}
		d := deviceRe.FindStringSubmatch(l)
		if d == nil || strings.HasPrefix(d[2], "no module installed") {
			continue
		}
		rest := d[2]
		mod := model.Module{Slot: d[1], Type: strings.Fields(rest)[0]}
		if s := sizeRe.FindStringSubmatch(rest); s != nil {
			mod.SizeB = bytesOf(s[1], s[2])
		}
		if s := specRe.FindStringSubmatch(rest); s != nil {
			mod.SpecMTs = atoi(s[1])
			if a := actualRe.FindStringSubmatch(rest); a != nil {
				mod.ActualMTs = atoi(a[1])
			}
		} else if s := speedRe.FindStringSubmatch(rest); s != nil {
			mod.SpecMTs = atoi(s[1])
		}
		m.Modules = append(m.Modules, mod)
	}
	return m
}

func atoi(s string) *int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return nil
	}
	return &n
}

func bytesOf(num, unit string) uint64 {
	f, _ := strconv.ParseFloat(num, 64)
	mult := map[string]float64{"KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30, "TiB": 1 << 40}[unit]
	return uint64(f * mult)
}
