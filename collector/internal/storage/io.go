package storage

import (
	"strconv"
	"strings"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

type ioStat struct{ read, written, ticks uint64 }

// IO calcula lectura, escritura y ocupación por disco con /proc/diskstats.
type IO struct {
	prev map[string]ioStat
	at   time.Time
}

// Sample completa ReadBps, WriteBps y BusyPct (desde la segunda muestra).
func (s *IO) Sample(env source.Env, disks []model.Disk) {
	now := env.Now()
	cur := map[string]ioStat{}
	lines, _ := env.FS.Lines("/proc/diskstats")
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) < 13 {
			continue
		}
		rd, _ := strconv.ParseUint(f[5], 10, 64)
		wr, _ := strconv.ParseUint(f[9], 10, 64)
		tk, _ := strconv.ParseUint(f[12], 10, 64)
		cur[f[2]] = ioStat{rd * 512, wr * 512, tk}
	}
	dt := now.Sub(s.at).Seconds()
	for i := range disks {
		d := &disks[i]
		d.ReadBps, d.WriteBps, d.BusyPct = nil, nil, nil
		c, ok := cur[d.Name]
		p, had := s.prev[d.Name]
		if !ok || !had || dt <= 0 {
			continue
		}
		rb := rate(p.read, c.read, dt)
		wb := rate(p.written, c.written, dt)
		busy := min(rate(p.ticks, c.ticks, dt)/1000*100, 100)
		d.ReadBps, d.WriteBps, d.BusyPct = &rb, &wb, &busy
	}
	s.prev, s.at = cur, now
}

func rate(a, b uint64, dt float64) float64 {
	if b < a {
		return 0
	}
	return float64(b-a) / dt
}
