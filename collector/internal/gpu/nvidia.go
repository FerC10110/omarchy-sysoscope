package gpu

import (
	"context"
	"strconv"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

const (
	smiQuery = "--query-gpu=pci.bus_id,name,driver_version,vbios_version,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw,power.limit,clocks.gr,clocks.mem,fan.speed"
	smiProcs = "--query-compute-apps=gpu_bus_id,pid,used_memory"
	smiFmt   = "--format=csv,noheader,nounits"
)

// Argumentos de nvidia-smi (también los usa la captura de fixtures).
var (
	SMIArgs     = []string{smiQuery, smiFmt}
	SMIProcArgs = []string{smiProcs, smiFmt}
)

// smiRow es una línea de nvidia-smi; los campos [N/A] quedan en nil.
type smiRow struct {
	bus, name, driver, vbios          string
	usage, memUsed, memTotal, temp    *float64
	power, powerLimit, core, mem, fan *float64
}

func (s *Sampler) querySMI(env source.Env, r *collect.Report) {
	out, err := env.Run.Run(context.Background(), "nvidia-smi", SMIArgs...)
	if err != nil {
		r.FromErr("gpu.metrics", "nvidia-smi", err)
		s.last = nil
		return
	}
	s.last = map[string]smiRow{}
	for _, row := range parseSMI(string(out)) {
		s.last[busKey(row.bus)] = row
	}
	s.procs = nil
	if s.Procs {
		s.queryProcs(env)
	}
}

// queryProcs lee los procesos que usan cada GPU.
func (s *Sampler) queryProcs(env source.Env) {
	s.procs = nil
	out, err := env.Run.Run(context.Background(), "nvidia-smi", SMIProcArgs...)
	if err != nil {
		return
	}
	s.procs = map[string][]model.GPUProc{}
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := splitCSV(l)
		if len(f) < 3 {
			continue
		}
		pid, err := strconv.Atoi(f[1])
		if err != nil {
			continue
		}
		mib, _ := strconv.ParseUint(f[2], 10, 64)
		name, _ := env.FS.Read("/proc/" + f[1] + "/comm")
		k := busKey(f[0])
		s.procs[k] = append(s.procs[k], model.GPUProc{PID: pid, Name: strings.TrimSpace(name), VRAMB: mib << 20})
	}
}

func splitCSV(l string) []string {
	f := strings.Split(l, ",")
	for i := range f {
		f[i] = strings.TrimSpace(f[i])
	}
	return f
}

func parseSMI(text string) []smiRow {
	var rows []smiRow
	for _, l := range strings.Split(strings.TrimSpace(text), "\n") {
		f := splitCSV(l)
		if len(f) < 13 {
			continue
		}
		rows = append(rows, smiRow{
			bus: f[0], name: f[1], driver: f[2], vbios: f[3],
			usage: num(f[4]), memUsed: num(f[5]), memTotal: num(f[6]), temp: num(f[7]),
			power: num(f[8]), powerLimit: num(f[9]), core: num(f[10]), mem: num(f[11]), fan: num(f[12]),
		})
	}
	return rows
}

func num(s string) *float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}

// busKey normaliza "00000000:42:00.0" y "0000:42:00.0" a "42:00.0".
func busKey(bus string) string {
	bus = strings.ToLower(bus)
	if i := strings.Index(bus, ":"); i >= 0 && strings.Count(bus, ":") == 2 {
		return bus[i+1:]
	}
	return bus
}

func applySMI(g *model.GPU, row smiRow) {
	if row.name != "" {
		g.Name = row.name
	}
	g.DriverVersion, g.VBIOS = row.driver, row.vbios
	g.UsagePct, g.TempC, g.PowerW, g.PowerLimitW = row.usage, row.temp, row.power, row.powerLimit
	g.CoreMHz, g.MemMHz, g.FanPct = row.core, row.mem, row.fan
	g.VRAMUsedB, g.VRAMTotalB = mib(row.memUsed), mib(row.memTotal)
}

func mib(v *float64) *uint64 {
	if v == nil {
		return nil
	}
	b := uint64(*v) << 20
	return &b
}
