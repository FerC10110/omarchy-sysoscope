// Package network lee las interfaces (/sys/class/net), sus tasas
// (/proc/net/dev), la puerta de enlace, los DNS y el estado del Wi-Fi.
package network

import (
	"encoding/binary"
	"encoding/hex"
	"net"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

const sysNet = "/sys/class/net/"

// Static lista las interfaces con su tipo, si son virtuales y su MAC.
func Static(env source.Env, r *collect.Report) []model.Iface {
	names, err := env.FS.Dir(sysNet)
	if err != nil {
		r.Absent("network")
		return []model.Iface{}
	}
	out := make([]model.Iface, 0, len(names))
	for _, n := range names {
		d := sysNet + n
		typ, _ := env.FS.ReadInt(d + "/type")
		i := model.Iface{Name: n, Virtual: !env.FS.Exists(d + "/device")}
		switch {
		case typ == 772:
			i.Kind = "loopback"
		case env.FS.Exists(d + "/wireless"):
			i.Kind = "wifi"
		case env.FS.Exists(d + "/bridge"):
			i.Kind = "bridge"
		case strings.HasPrefix(n, "tun") || strings.HasPrefix(n, "wg") || strings.HasPrefix(n, "tailscale") || strings.HasPrefix(n, "tap"):
			i.Kind = "vpn"
		case i.Virtual:
			i.Kind = "virtual"
		default:
			i.Kind = "ethernet"
		}
		if mac, err := env.FS.Read(d + "/address"); err == nil && mac != "00:00:00:00:00:00" {
			i.MAC = strings.TrimSpace(mac)
		}
		out = append(out, i)
	}
	return out
}

type counters struct{ rx, tx uint64 }

// Sampler calcula tasas con la lectura anterior de /proc/net/dev. Addrs
// devuelve las direcciones de una interfaz (nil = la biblioteca estándar).
type Sampler struct {
	Addrs   func(name string) (v4, v6 []string)
	prev    map[string]counters
	at      time.Time
	noSpeed map[string]bool // interfaces activas sin velocidad (Wi-Fi, lo)
}

// Sample actualiza estado, velocidad y tasas en cada muestra; con slow
// también direcciones, puerta de enlace, DNS y Wi-Fi.
func (s *Sampler) Sample(env source.Env, n *model.Network, slow bool, r *collect.Report) {
	now := env.Now()
	if s.noSpeed == nil || slow {
		s.noSpeed = map[string]bool{}
	}
	cur := readDev(env)
	dt := now.Sub(s.at).Seconds()
	n.RxBps, n.TxBps = 0, 0
	for i := range n.Interfaces {
		it := &n.Interfaces[i]
		d := sysNet + it.Name
		op, _ := env.FS.Read(d + "/operstate")
		it.Up = op == "up"
		if op == "unknown" { // loopback y algunos túneles: se mira IFF_UP
			flags, _ := env.FS.Read(d + "/flags")
			f, _ := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(flags), "0x"), 16, 64)
			it.Up = f&1 == 1
		}
		if !it.Up {
			it.SpeedMbps = nil
		} else if slow || it.SpeedMbps == nil && !s.noSpeed[it.Name] {
			// la velocidad del enlace casi no cambia: se lee en el ritmo lento
			it.SpeedMbps = nil
			if sp, err := env.FS.ReadInt(d + "/speed"); err == nil && sp > 0 {
				v := int(sp)
				it.SpeedMbps = &v
			} else {
				s.noSpeed[it.Name] = true
			}
		}
		it.RxBps, it.TxBps = nil, nil
		c, ok := cur[it.Name]
		if !ok {
			continue
		}
		it.RxTotalB, it.TxTotalB = c.rx, c.tx
		if p, had := s.prev[it.Name]; had && dt > 0 {
			rx, tx := rate(p.rx, c.rx, dt), rate(p.tx, c.tx, dt)
			it.RxBps, it.TxBps = &rx, &tx
			if !it.Virtual && it.Kind != "loopback" && it.Up {
				n.RxBps += rx
				n.TxBps += tx
			}
		}
	}
	s.prev, s.at = cur, now
	if !slow {
		return
	}
	addrs := s.Addrs
	if addrs == nil {
		addrs = netlinkAddrs(env, n.Interfaces)
	}
	for i := range n.Interfaces {
		it := &n.Interfaces[i]
		it.IPv4, it.IPv6 = addrs(it.Name)
	}
	n.Gateway, n.GatewayIface = gateway(env)
	n.DNS = dns(env)
	wifi(env, n, r)
}

func rate(a, b uint64, dt float64) float64 {
	if b < a {
		return 0
	}
	return float64(b-a) / dt
}

// readDev interpreta /proc/net/dev ("enp5s0: 123 …" o "enp5s0:123 …").
func readDev(env source.Env) map[string]counters {
	out := map[string]counters{}
	lines, _ := env.FS.Lines("/proc/net/dev")
	for _, l := range lines {
		name, rest, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 9 {
			continue
		}
		rx, _ := strconv.ParseUint(f[0], 10, 64)
		tx, _ := strconv.ParseUint(f[8], 10, 64)
		out[strings.TrimSpace(name)] = counters{rx, tx}
	}
	return out
}

// netlinkAddrs trae todas las direcciones en un solo volcado RTM_GETADDR
// (net.InterfaceByName hace dos volcados por interfaz) y las agrupa por
// nombre de interfaz, con el índice leído de sysfs.
func netlinkAddrs(env source.Env, ifaces []model.Iface) func(string) ([]string, []string) {
	byIndex := map[uint32]string{}
	for _, it := range ifaces {
		if idx, err := env.FS.ReadInt(sysNet + it.Name + "/ifindex"); err == nil {
			byIndex[uint32(idx)] = it.Name
		}
	}
	v4, v6 := map[string][]string{}, map[string][]string{}
	tab, err := syscall.NetlinkRIB(syscall.RTM_GETADDR, syscall.AF_UNSPEC)
	if err == nil {
		msgs, _ := syscall.ParseNetlinkMessage(tab)
		for i := range msgs {
			m := &msgs[i]
			if m.Header.Type != syscall.RTM_NEWADDR || len(m.Data) < syscall.SizeofIfAddrmsg {
				continue
			}
			family, prefix := m.Data[0], m.Data[1]
			name, ok := byIndex[binary.NativeEndian.Uint32(m.Data[4:8])]
			if !ok {
				continue
			}
			attrs, err := syscall.ParseNetlinkRouteAttr(m)
			if err != nil {
				continue
			}
			var ip net.IP
			for _, a := range attrs {
				switch a.Attr.Type {
				case syscall.IFA_LOCAL:
					ip = net.IP(a.Value)
				case syscall.IFA_ADDRESS:
					if ip == nil {
						ip = net.IP(a.Value)
					}
				}
			}
			if ip == nil {
				continue
			}
			cidr := ip.String() + "/" + strconv.Itoa(int(prefix))
			if family == syscall.AF_INET {
				v4[name] = append(v4[name], cidr)
			} else {
				v6[name] = append(v6[name], cidr)
			}
		}
	}
	return func(name string) ([]string, []string) { return v4[name], v6[name] }
}

// gateway toma la ruta por defecto (destino 00000000) de /proc/net/route; la
// dirección está en hex little-endian.
func gateway(env source.Env) (string, string) {
	lines, _ := env.FS.Lines("/proc/net/route")
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) < 3 || f[1] != "00000000" {
			continue
		}
		b, err := hex.DecodeString(f[2])
		if err != nil || len(b) != 4 {
			continue
		}
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, binary.LittleEndian.Uint32(b))
		return ip.String(), f[0]
	}
	return "", ""
}

// dns lee los servidores reales (systemd-resolved si está; si no,
// /etc/resolv.conf), sin el stub local 127.0.0.53.
func dns(env source.Env) []string {
	lines, err := env.FS.Lines("/run/systemd/resolve/resolv.conf")
	if err != nil {
		lines, _ = env.FS.Lines("/etc/resolv.conf")
	}
	var out []string
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) >= 2 && f[0] == "nameserver" && f[1] != "127.0.0.53" && f[1] != "127.0.0.54" {
			out = append(out, f[1])
		}
	}
	return out
}
