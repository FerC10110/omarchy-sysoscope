package view

import (
	"sort"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

func init() {
	tops["cpu"] = func(s *model.Snapshot, o Options) []Block {
		return topApps(s, o, func(a model.App) (float64, bool) { return a.CPUPct, true }, Pct)
	}
	tops["memory"] = func(s *model.Snapshot, o Options) []Block {
		return topApps(s, o, func(a model.App) (float64, bool) { return float64(a.MemB), true },
			func(v float64) string { return Bytes(uint64(v)) })
	}
	tops["gpu"] = func(s *model.Snapshot, o Options) []Block {
		bs := topApps(s, o, func(a model.App) (float64, bool) {
			if a.GPUPct == nil {
				return 0, false
			}
			return *a.GPUPct, true
		}, Pct)
		if bs == nil && s.Apps != nil && len(s.Apps.List) > 0 {
			if u := Unavailable(s, "apps.gpu"); u != nil {
				return []Block{Group(i18n.T(o.Lang, "Apps using the most"), *u)}
			}
		}
		return bs
	}
	tops["network"] = func(s *model.Snapshot, o Options) []Block {
		bs := topApps(s, o, func(a model.App) (float64, bool) {
			if a.NetRxBps == nil || a.NetTxBps == nil {
				return 0, false
			}
			return *a.NetRxBps + *a.NetTxBps, true
		}, Rate)
		if bs != nil {
			bs[0].Blocks = append(bs[0].Blocks, Text(tcpNote(o.Lang), ""))
		}
		return bs
	}
}

// topApps: las 3 apps con más value (mayor que 0), cada una un link a la
// sección Aplicaciones. nil si no hay ninguna.
func topApps(s *model.Snapshot, o Options, value func(model.App) (float64, bool), format func(float64) string) []Block {
	if s.Apps == nil {
		return nil
	}
	type top struct {
		name string
		v    float64
	}
	var ts []top
	for _, a := range s.Apps.List {
		if v, ok := value(a); ok && v > 0 {
			ts = append(ts, top{a.Name, v})
		}
	}
	if len(ts) == 0 {
		return nil
	}
	sort.SliceStable(ts, func(i, j int) bool { return ts[i].v > ts[j].v })
	links := make([]Block, 0, 3)
	for _, t := range ts[:min(3, len(ts))] {
		links = append(links, Link(t.name+" · "+format(t.v), "", "apps"))
	}
	return []Block{Group(i18n.T(o.Lang, "Apps using the most"), links...)}
}
