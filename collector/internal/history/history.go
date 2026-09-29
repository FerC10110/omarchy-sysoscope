// Package history guarda el historial de métricas en buckets con el valor
// máximo de cada intervalo: 24 h a 1 min, 7 días a 10 min y 30 días a 1 h
// (el máximo conserva los picos que un promedio escondería).
package history

import (
	"math"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

// Metrics es el orden fijo de las métricas del historial (parte del formato).
var Metrics = []string{"cpu.usage", "cpu.temp", "mem.used_pct", "swap.used_pct",
	"gpu.usage", "gpu.temp", "gpu.vram_pct", "gpu.power", "disk.read", "disk.write",
	"net.rx", "net.tx", "load1", "bat.pct", "power.w"}

type Resolution struct {
	Name  string
	Step  time.Duration
	Slots int
}

var Resolutions = []Resolution{{"24h", time.Minute, 1440}, {"7d", 10 * time.Minute, 1008}, {"30d", time.Hour, 720}}

// Point es un valor del historial; V nil = sin dato.
type Point struct {
	T time.Time
	V *float64
}

type ring struct {
	res    Resolution
	bucket []int64     // inicio del bucket (unix) de cada slot
	vals   [][]float32 // [slot][métrica], NaN = sin dato
}

func newRing(res Resolution) *ring {
	r := &ring{res: res, bucket: make([]int64, res.Slots), vals: make([][]float32, res.Slots)}
	for i := range r.vals {
		r.vals[i] = nanRow()
	}
	return r
}

func nanRow() []float32 {
	row := make([]float32, len(Metrics))
	for i := range row {
		row[i] = float32(math.NaN())
	}
	return row
}

func (r *ring) slot(t time.Time) (int, int64) {
	step := int64(r.res.Step / time.Second)
	b := t.Unix() / step
	return int(b % int64(r.res.Slots)), b * step
}

// Store son los tres anillos en memoria.
type Store struct {
	rings []*ring
}

func New() *Store {
	s := &Store{}
	for _, res := range Resolutions {
		s.rings = append(s.rings, newRing(res))
	}
	return s
}

var index = func() map[string]int {
	m := map[string]int{}
	for i, n := range Metrics {
		m[n] = i
	}
	return m
}()

// Add suma una muestra: cada métrica queda con el máximo de su bucket en las
// tres resoluciones. Un slot de otro período se limpia antes.
func (s *Store) Add(t time.Time, values map[string]float64) {
	for _, r := range s.rings {
		i, start := r.slot(t)
		if r.bucket[i] != start {
			r.bucket[i] = start
			r.vals[i] = nanRow()
		}
		row := r.vals[i]
		for name, v := range values {
			k, ok := index[name]
			if !ok || math.IsNaN(v) || math.IsInf(v, 0) {
				continue
			}
			if cur := row[k]; math.IsNaN(float64(cur)) || float32(v) > cur {
				row[k] = float32(v)
			}
		}
	}
}

// Series devuelve los puntos de span que terminan en el bucket de now, con
// la resolución más fina que lo cubra.
func (s *Store) Series(metric string, span time.Duration, now time.Time) []Point {
	k, ok := index[metric]
	if !ok {
		return nil
	}
	r := s.rings[len(s.rings)-1]
	for _, c := range s.rings {
		if c.res.Step*time.Duration(c.res.Slots) >= span {
			r = c
			break
		}
	}
	n := int(span / r.res.Step)
	if n > r.res.Slots {
		n = r.res.Slots
	}
	step := int64(r.res.Step / time.Second)
	endBucket := now.Unix() / step
	pts := make([]Point, n)
	for j := 0; j < n; j++ {
		b := endBucket - int64(n-1-j)
		start := b * step
		p := Point{T: time.Unix(start, 0).UTC()}
		i := int(b % int64(r.res.Slots))
		if r.bucket[i] == start {
			if v := r.vals[i][k]; !math.IsNaN(float64(v)) {
				f := float64(v)
				p.V = &f
			}
		}
		pts[j] = p
	}
	return pts
}

// Values extrae las métricas del historial de un Snapshot. Una métrica sin
// dato no aparece.
func Values(s *model.Snapshot) map[string]float64 {
	v := map[string]float64{}
	put := func(k string, p *float64) {
		if p != nil {
			v[k] = *p
		}
	}
	if c := s.CPU; c != nil {
		put("cpu.usage", c.UsagePct)
		put("cpu.temp", c.TempC)
		v["load1"] = c.Load[0]
	}
	if m := s.Memory; m != nil && m.TotalB > 0 {
		v["mem.used_pct"] = m.UsedPct
		if m.SwapTotalB > 0 {
			v["swap.used_pct"] = float64(m.SwapUsedB) / float64(m.SwapTotalB) * 100
		}
	}
	if g := mainGPU(s.GPUs); g != nil {
		put("gpu.usage", g.UsagePct)
		put("gpu.temp", g.TempC)
		put("gpu.power", g.PowerW)
		if g.VRAMUsedB != nil && g.VRAMTotalB != nil && *g.VRAMTotalB > 0 {
			v["gpu.vram_pct"] = float64(*g.VRAMUsedB) / float64(*g.VRAMTotalB) * 100
		}
	}
	if st := s.Storage; st != nil {
		var rd, wr float64
		var any bool
		for _, d := range st.Disks {
			if d.ReadBps != nil {
				rd, any = rd+*d.ReadBps, true
			}
			if d.WriteBps != nil {
				wr, any = wr+*d.WriteBps, true
			}
		}
		if any {
			v["disk.read"], v["disk.write"] = rd, wr
		}
	}
	if n := s.Network; n != nil {
		v["net.rx"], v["net.tx"] = n.RxBps, n.TxBps
	}
	if p := s.Power; p != nil {
		for _, b := range p.Batteries {
			if !b.Peripheral {
				v["bat.pct"] = b.Pct
				if len(p.RAPL) == 0 && b.PowerW != nil {
					v["power.w"] = *b.PowerW
				}
				break
			}
		}
		if len(p.RAPL) > 0 {
			var w float64
			for _, d := range p.RAPL {
				if d.Name == "package-0" || len(p.RAPL) == 1 {
					w += d.Watts
				}
			}
			v["power.w"] = w
		}
	}
	return v
}

// mainGPU es la primera GPU dedicada activa, o la primera activa.
func mainGPU(gs []model.GPU) *model.GPU {
	var first *model.GPU
	for i := range gs {
		g := &gs[i]
		if g.Suspended {
			continue
		}
		if !g.Integrated {
			return g
		}
		if first == nil {
			first = g
		}
	}
	return first
}
