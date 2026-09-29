package view

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/power"
)

func init() {
	builders["storage"] = storageView
	builders["network"] = networkView
	builders["sensors"] = sensorsView
	builders["power"] = powerView
	actions["power"] = powerActions
}

// kvIf devuelve el kv solo si le queda alguna fila con valor.
func kvIf(title string, rs ...[2]string) []Block {
	if r := rows(rs...); len(r) > 0 {
		return []Block{KV(title, r...)}
	}
	return nil
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func intp(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

// diskReasons son los motivos por los que un disco requiere atención.
func diskReasons(m *model.Smart, l string) []string {
	why := slices.Clone(m.Warnings)
	if m.BadSectors != nil && *m.BadSectors > 0 {
		why = append(why, i18n.F(l, "%d bad sectors", *m.BadSectors))
	}
	if m.MediaErrors != nil && *m.MediaErrors > 0 {
		why = append(why, i18n.F(l, "%d media errors", *m.MediaErrors))
	}
	return why
}

func storageView(s *model.Snapshot, o Options) []Block {
	l := o.Lang
	st := s.Storage
	if st == nil || (len(st.Disks) == 0 && len(st.Filesystems) == 0) {
		return appendUnavailable([]Block{}, s, "storage")
	}
	var bs []Block
	for _, d := range st.Disks {
		bs = append(bs, Group(d.Model+" ("+d.Name+")", diskBlocks(s, d, o)...))
	}
	for _, f := range st.Filesystems {
		var at []string
		for _, m := range f.Mounts {
			if m == "/" || m == "/home" || m == "/boot" {
				at = append(at, m)
			}
		}
		if len(at) > 0 {
			bs = append(bs, Meter(strings.Join(at, ", "), f.UsedPct/100, i18n.F(l, "%s free", Bytes(f.FreeB)), Level(f.UsedPct, 85, 95)))
		}
	}
	var fr []Row
	for _, f := range st.Filesystems {
		typ := f.FSType
		if f.Crypt {
			typ += " · LUKS"
		}
		if f.LVM {
			typ += " · LVM"
		}
		fr = append(fr, Row{ID: f.Device, Cells: []string{strings.Join(f.Mounts, ", "), f.Device, typ,
			Pct(f.UsedPct) + " (" + Bytes(f.UsedB) + ")", Bytes(f.SizeB)}})
	}
	bs = append(bs, Table(i18n.T(l, "Filesystems"), []string{i18n.T(l, "Mount"), i18n.T(l, "Device"), i18n.T(l, "Type"), i18n.T(l, "Used"), i18n.T(l, "Size")}, fr, false))
	if st.PSI != nil {
		bs = append(bs, KV("", [2]string{i18n.T(l, "I/O pressure (10 s)"), Pct(st.PSI.Some10)}))
	}
	return bs
}

func diskBlocks(s *model.Snapshot, d model.Disk, o Options) []Block {
	l := o.Lang
	iface := d.Transport
	if d.Rotational {
		iface += ", HDD"
	}
	temp := ""
	if d.TempC != nil {
		temp = Temp(*d.TempC, o.Units)
	} else if d.Smart != nil && d.Smart.TempC != nil {
		temp = Temp(*d.Smart.TempC, o.Units)
	}
	var rd, wr, busy string
	if d.ReadBps != nil {
		rd = Rate(*d.ReadBps)
	}
	if d.WriteBps != nil {
		wr = Rate(*d.WriteBps)
	}
	if d.BusyPct != nil {
		busy = Pct(*d.BusyPct)
	}
	bs := kvIf("",
		[2]string{i18n.T(l, "Size"), Bytes(d.SizeB)},
		[2]string{i18n.T(l, "Interface"), iface},
		[2]string{i18n.T(l, "Serial"), d.Serial},
		[2]string{i18n.T(l, "Temperature"), temp},
	)
	bs = append(bs, kvIf(i18n.T(l, "Now"),
		[2]string{i18n.T(l, "Read"), rd},
		[2]string{i18n.T(l, "Write"), wr},
		[2]string{i18n.T(l, "Busy"), busy},
	)...)
	m := d.Smart
	if m == nil {
		return appendUnavailable(bs, s, "storage.smart")
	}
	status := i18n.T(l, "OK")
	why := diskReasons(m, l)
	switch {
	case m.Failing:
		status = i18n.T(l, "Failing")
	case len(why) > 0:
		status = i18n.T(l, "Warnings")
	}
	wear := ""
	if m.PercentUsed != nil {
		wear = fmt.Sprintf("%d%%", *m.PercentUsed)
	}
	bs = append(bs, KV(i18n.T(l, "Health"), rows(
		[2]string{i18n.T(l, "Status"), status},
		[2]string{i18n.T(l, "Wear"), wear},
		[2]string{i18n.T(l, "Power-on hours"), intp(m.PowerOnHours)},
		[2]string{i18n.T(l, "Bad sectors"), intp(m.BadSectors)},
		[2]string{i18n.T(l, "Media errors"), intp(m.MediaErrors)},
	)...))
	if m.Failing {
		bs = append(bs, Text(i18n.T(l, "SMART says this disk is failing: back it up now"), "crit"))
	}
	for _, w := range why {
		bs = append(bs, Text(w, "warn"))
	}
	return bs
}

func networkView(s *model.Snapshot, o Options) []Block {
	l := o.Lang
	n := s.Network
	if n == nil {
		return appendUnavailable([]Block{}, s, "network")
	}
	gw := n.Gateway
	if gw != "" && n.GatewayIface != "" {
		gw = i18n.F(l, "%s via %s", gw, n.GatewayIface)
	}
	bs := []Block{
		Spark(i18n.T(l, "Download"), o.recent("net.rx"), Rate(n.RxBps)),
		Spark(i18n.T(l, "Upload"), o.recent("net.tx"), Rate(n.TxBps)),
	}
	bs = append(bs, kvIf("", [2]string{i18n.T(l, "Gateway"), gw}, [2]string{"DNS", strings.Join(n.DNS, ", ")})...)
	ifs := slices.Clone(n.Interfaces)
	// Las caídas y las virtuales van al final.
	late := func(i model.Iface) int {
		if i.Virtual || !i.Up {
			return 1
		}
		return 0
	}
	slices.SortStableFunc(ifs, func(a, b model.Iface) int { return late(a) - late(b) })
	for _, i := range ifs {
		if i.Kind == "loopback" {
			continue
		}
		bs = append(bs, Group(i.Name, ifaceBlocks(i, l)...))
	}
	return bs
}

func ifaceBlocks(i model.Iface, l string) []Block {
	kind := i.Kind
	if i.Virtual {
		kind += " " + i18n.T(l, "(virtual)")
	}
	state := i18n.T(l, "Down")
	if i.Up {
		state = i18n.T(l, "Up")
	}
	speed, traffic := "", ""
	if i.SpeedMbps != nil && *i.SpeedMbps > 0 {
		speed = fmt.Sprintf("%d Mb/s", *i.SpeedMbps)
	}
	if i.RxBps != nil && i.TxBps != nil {
		traffic = "↓ " + Rate(*i.RxBps) + " · ↑ " + Rate(*i.TxBps)
	}
	rs := [][2]string{
		{i18n.T(l, "Type"), kind},
		{i18n.T(l, "State"), state},
		{"MAC", i.MAC},
		{i18n.T(l, "Speed"), speed},
		{"IPv4", strings.Join(i.IPv4, ", ")},
		{"IPv6", strings.Join(i.IPv6, ", ")},
		{i18n.T(l, "Traffic"), traffic},
		{i18n.T(l, "Total"), "↓ " + Bytes(i.RxTotalB) + " · ↑ " + Bytes(i.TxTotalB)},
	}
	if w := i.Wifi; w != nil {
		sig, rate := "", ""
		if w.SignalDBm != nil {
			sig = fmt.Sprintf("%d dBm", *w.SignalDBm)
		}
		if w.BitrateMbps != nil {
			rate = fmt.Sprintf("%.1f Mb/s", *w.BitrateMbps)
		}
		rs = append(rs, [2]string{"SSID", w.SSID}, [2]string{i18n.T(l, "Signal"), sig}, [2]string{i18n.T(l, "Bitrate"), rate})
	}
	bs := []Block{KV("", rows(rs...)...)}
	if i.Kind == "wifi" {
		bs = append(bs, wifiBlocks(i, l)...)
	}
	return bs
}

func sensorsView(s *model.Snapshot, o Options) []Block {
	l := o.Lang
	if s.Sensors == nil || len(s.Sensors.Chips) == 0 {
		return appendUnavailable([]Block{}, s, "sensors")
	}
	temp := func(v *float64) string {
		if v == nil {
			return "—"
		}
		return Temp(*v, o.Units)
	}
	var bs []Block
	for _, c := range s.Sensors.Chips {
		var rs []Row
		var alerts []Block
		for _, r := range c.Temps {
			rs = append(rs, Row{ID: "t:" + r.Label, Cells: []string{r.Label, Temp(r.Value, o.Units), temp(r.High), temp(r.Crit)}})
			switch {
			case r.Crit != nil && r.Value >= *r.Crit:
				alerts = append(alerts, Text(i18n.F(l, "%s: above its critical mark", r.Label), "crit"))
			case r.High != nil && r.Value >= *r.High:
				alerts = append(alerts, Text(i18n.F(l, "%s: above its high mark", r.Label), "warn"))
			}
		}
		for _, r := range c.Fans {
			rs = append(rs, Row{ID: "f:" + r.Label, Cells: []string{r.Label, fmt.Sprintf("%.0f RPM", r.Value), "—", "—"}})
		}
		for _, r := range c.Volts {
			rs = append(rs, Row{ID: "v:" + r.Label, Cells: []string{r.Label, fmt.Sprintf("%.3f V", r.Value), "—", "—"}})
		}
		blocks := append([]Block{Table("", []string{i18n.T(l, "Sensor"), i18n.T(l, "Value"), i18n.T(l, "High"), i18n.T(l, "Critical")}, rs, false)}, alerts...)
		bs = append(bs, Group(dash(c.Label), blocks...))
	}
	return bs
}

func powerView(s *model.Snapshot, o Options) []Block {
	l := o.Lang
	p := s.Power
	var bs []Block
	if p == nil {
		p = &model.Power{}
	}
	profile, ac := p.Profile, ""
	if profile != "" && len(p.Profiles) > 0 {
		profile += " (" + i18n.F(l, "available: %s", strings.Join(p.Profiles, ", ")) + ")"
	}
	if p.OnAC != nil {
		ac = i18n.T(l, "No")
		if *p.OnAC {
			ac = i18n.T(l, "Yes")
		}
	}
	rs := [][2]string{{i18n.T(l, "Profile"), profile}, {i18n.T(l, "On AC"), ac}}
	if len(p.Profiles) > 0 && power.HasBattery(p) {
		rs = append(rs, [2]string{i18n.T(l, "Saved profiles"),
			i18n.F(l, "Plugged in: %s · Battery: %s", dash(p.SavedAC), dash(p.SavedBattery))})
	}
	bs = append(bs, kvIf("", rs...)...)
	var per []Row
	for _, b := range p.Batteries {
		name := b.Model
		if name == "" {
			name = b.Name
		}
		if b.Peripheral {
			per = append(per, Row{ID: b.Name, Cells: []string{name, Pct(b.Pct), batteryStatus(b.Status, l)}})
			continue
		}
		bs = append(bs, Group(name, batteryBlocks(b, l)...))
	}
	if len(per) > 0 {
		bs = append(bs, Table(i18n.T(l, "Peripherals"), []string{i18n.T(l, "Device"), i18n.T(l, "Charge"), i18n.T(l, "Status")}, per, false))
	}
	var draw [][2]string
	for _, d := range p.RAPL {
		draw = append(draw, [2]string{d.Name, Watts(d.Watts)})
	}
	for _, g := range s.GPUs {
		if g.PowerW != nil {
			draw = append(draw, [2]string{g.Name, Watts(*g.PowerW)})
		}
	}
	if len(draw) > 0 {
		bs = append(bs, KV(i18n.T(l, "Measured draw"), draw...))
	}
	bs = append(bs, sleepBlocks(p, l, Unavailable(s, "power.inhibitors") == nil && Unavailable(s, "power.idle_windows") == nil)...)
	bs = appendUnavailable(bs, s, "power.rapl")
	bs = appendUnavailable(bs, s, "power.profile")
	bs = appendUnavailable(bs, s, "power.inhibitors")
	bs = appendUnavailable(bs, s, "power.idle_windows")
	if bs == nil {
		bs = appendUnavailable([]Block{}, s, "power")
	}
	return bs
}

// powerActions: p cambia el perfil (Omarchy lo recuerda para la fuente de
// ahora); en una laptop con el script de Omarchy, P guarda el de la otra
// fuente sin cambiar el activo. Sin power-profiles-daemon, ninguna.
func powerActions(s *model.Snapshot, o Options) []Action {
	l := o.Lang
	out := []Action{}
	p := s.Power
	if p == nil || len(p.Profiles) == 0 {
		return out
	}
	active := p.Profile
	if !slices.Contains(p.Profiles, active) {
		active = p.Profiles[0]
	}
	out = append(out, Action{Key: "p", Label: i18n.T(l, "Power profile"), ID: "power.profile", Target: "section", Mode: "background",
		Input: &Input{Type: "choice", Prompt: i18n.T(l, "Profile"), Options: slices.Clone(p.Profiles), Default: active}})
	if !p.SetScript || !power.HasBattery(p) {
		return out
	}
	label, saved := i18n.T(l, "Profile on battery"), p.SavedBattery
	if power.Other(power.Source(p)) == "ac" {
		label, saved = i18n.T(l, "Profile when plugged in"), p.SavedAC
	}
	if !slices.Contains(p.Profiles, saved) {
		saved = active
	}
	return append(out, Action{Key: "P", Label: label, ID: "power.profile_other", Target: "section", Mode: "background",
		Input: &Input{Type: "choice", Prompt: label, Options: slices.Clone(p.Profiles), Default: saved}})
}

func batteryBlocks(b model.Battery, l string) []Block {
	health, limit, draw, left := "", "", "", ""
	if b.HealthPct != nil {
		health = Pct(*b.HealthPct)
		if b.FullWh != nil && b.DesignWh != nil {
			health += " (" + i18n.F(l, "%s of %s", fmt.Sprintf("%.1f", *b.FullWh), fmt.Sprintf("%.1f Wh", *b.DesignWh)) + ")"
		}
	}
	if b.ChargeLimitPct != nil {
		limit = fmt.Sprintf("%d%%", *b.ChargeLimitPct)
	}
	if b.PowerW != nil {
		draw = Watts(*b.PowerW)
	}
	if b.TimeLeftS != nil {
		left = Duration(float64(*b.TimeLeftS), l)
	}
	return []Block{
		Meter(i18n.T(l, "Charge"), b.Pct/100, Pct(b.Pct)+" · "+batteryStatus(b.Status, l), Level(100-b.Pct, 80, 90)),
		KV("", rows(
			[2]string{i18n.T(l, "Health"), health},
			[2]string{i18n.T(l, "Cycles"), intp(b.Cycles)},
			[2]string{i18n.T(l, "Charge limit"), limit},
			[2]string{i18n.T(l, "Draw"), draw},
			[2]string{i18n.T(l, "Time left"), left},
		)...),
	}
}
