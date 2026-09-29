// Package ondisk saca de una muestra lo que identifica a la máquina, a su
// dueño o lo que hace, antes de escribirla en disco (latest.json y el caché
// del inventario). El panel la recibe entera por el stream; en disco no
// quedan: títulos de ventanas, inhibidores (quién y por qué), series de
// discos y monitores, MACs y nombres de interfaz hechos con la MAC,
// hostname, nombres de usuario y su carpeta, redes Wi-Fi (SSID, BSSID,
// UUID), nombres y direcciones Bluetooth, nombres de pactl (llevan la serie
// USB o la MAC), la MAC en el nombre de las baterías de periféricos, UUIDs
// de discos, IPs públicas e IPv6 (también en los orígenes permitidos de un
// puerto), y la salud (journal,
// volcados, OOM). Nunca toca la muestra que recibe: copia lo que cambia.
package ondisk

import (
	"net/netip"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

// uuidRe: un UUID, también escapado como en los nombres de unidades de
// systemd (\x2d en vez de -).
var uuidRe = regexp.MustCompile(`(?i)[0-9a-f]{8}(?:-|\\x2d)[0-9a-f]{4}(?:-|\\x2d)[0-9a-f]{4}(?:-|\\x2d)[0-9a-f]{4}(?:-|\\x2d)[0-9a-f]{12}`)

// macRe: una MAC (con :, - o _, como en los nombres de power_supply de
// Bluetooth o de pactl).
var macRe = regexp.MustCompile(`(?i)\b[0-9a-f]{2}([:_-])[0-9a-f]{2}(?:[:_-][0-9a-f]{2}){4}\b`)

// macIfaceRe: los nombres de interfaz que systemd arma con la MAC.
var macIfaceRe = regexp.MustCompile(`^(enx|wlx|wwx)[0-9a-f]{12}$`)

type scrub struct {
	home, user string
	ifaces     map[string]string
}

// Snapshot devuelve una copia de s sin los datos que identifican. home y
// user son la carpeta y el nombre del usuario que corre el colector.
func Snapshot(s *model.Snapshot, home, user string) *model.Snapshot {
	if s == nil {
		return nil
	}
	home = strings.TrimSuffix(home, "/")
	if user == "" && home != "" {
		user = path.Base(home) // sin nombre leído, el de su carpeta
	}
	x := &scrub{home: home, user: user, ifaces: map[string]string{}}
	c := *s
	if s.Memory != nil {
		m := *s.Memory
		m.Swaps = slices.Clone(m.Swaps)
		for i := range m.Swaps {
			m.Swaps[i].Name = x.path(m.Swaps[i].Name)
		}
		c.Memory = &m
	}
	if s.Storage != nil {
		st := *s.Storage
		st.Disks = slices.Clone(st.Disks)
		for i := range st.Disks {
			d := &st.Disks[i]
			d.Serial = ""
			d.Partitions = slices.Clone(d.Partitions)
			for j := range d.Partitions {
				p := &d.Partitions[j]
				p.Holder = x.path(p.Holder)
				p.Mounts = x.paths(p.Mounts)
			}
		}
		st.Filesystems = slices.Clone(st.Filesystems)
		for i := range st.Filesystems {
			f := &st.Filesystems[i]
			f.Device = x.path(f.Device)
			f.Mounts = x.paths(f.Mounts)
		}
		c.Storage = &st
	}
	if s.Network != nil {
		n := *s.Network
		n.Interfaces = slices.Clone(n.Interfaces)
		for i := range n.Interfaces {
			it := &n.Interfaces[i]
			it.Name = x.iface(it.Name)
			it.MAC = ""
			it.IPv4, it.IPv6 = x.addrs(it.IPv4), x.addrs(it.IPv6)
			if it.Wifi != nil {
				w := *it.Wifi
				w.SSID, w.Connecting, w.Error, w.Networks = "", "", "", nil
				it.Wifi = &w
			}
		}
		n.Gateway, n.GatewayIface, n.DNS = x.addr(n.Gateway), x.iface(n.GatewayIface), x.addrs(n.DNS)
		c.Network = &n
	}
	if s.Power != nil {
		p := *s.Power
		if p.Inhibit != nil {
			in := *p.Inhibit
			in.Logind = slices.Clone(in.Logind)
			for i := range in.Logind {
				in.Logind[i].Who, in.Logind[i].Why = "", ""
			}
			in.Windows = slices.Clone(in.Windows)
			for i := range in.Windows {
				in.Windows[i] = model.IdleWindow{}
			}
			p.Inhibit = &in
		}
		p.Batteries = slices.Clone(p.Batteries)
		for i := range p.Batteries {
			// hid-<MAC>-battery, ps-controller-battery-<MAC>
			p.Batteries[i].Name = x.path(p.Batteries[i].Name)
		}
		p.Wakeup = slices.Clone(p.Wakeup)
		for i := range p.Wakeup {
			if p.Wakeup[i].Kind == "net" {
				p.Wakeup[i].Name = x.iface(p.Wakeup[i].Name)
			}
		}
		c.Power = &p
	}
	if s.System != nil {
		y := System(s.System)
		y.FailedUnits = slices.Clone(y.FailedUnits)
		for i := range y.FailedUnits {
			u := &y.FailedUnits[i]
			u.Name, u.Description = x.path(u.Name), x.path(u.Description)
		}
		c.System = y
	}
	if s.Apps != nil {
		a := *s.Apps
		a.List = slices.Clone(a.List)
		for i := range a.List {
			ws := slices.Clone(a.List[i].Windows)
			for j := range ws {
				ws[j].Title = ""
			}
			a.List[i].Windows = ws
		}
		c.Apps = &a
	}
	if s.Maintenance != nil {
		m := *s.Maintenance
		m.Health = nil // journal, volcados y OOM: solo en vivo
		m.Caches = slices.Clone(m.Caches)
		for i := range m.Caches {
			m.Caches[i].Path = x.path(m.Caches[i].Path)
		}
		if m.Snapshots != nil {
			sn := *m.Snapshots
			sn.User = ""
			sn.Configs = slices.Clone(sn.Configs)
			for i := range sn.Configs {
				sn.Configs[i].AllowUsers = ""
			}
			m.Snapshots = &sn
		}
		c.Maintenance = &m
	}
	if s.Security != nil {
		sec := *s.Security
		if sec.Listening != nil {
			sec.Listening = slices.Clone(sec.Listening)
			for i := range sec.Listening {
				l := &sec.Listening[i]
				l.Addr, l.Iface, l.From = x.addr(l.Addr), x.iface(l.Iface), x.froms(l.From)
				l.Owners = slices.Clone(l.Owners)
				for j := range l.Owners {
					if o := &l.Owners[j]; humanUID(o.UID) {
						o.User = ""
					}
				}
			}
		}
		if sec.Firewall != nil {
			fw := *sec.Firewall
			fw.Rules = slices.Clone(fw.Rules)
			for i := range fw.Rules {
				r := &fw.Rules[i]
				r.To, r.From, r.Iface = x.addr(r.To), x.addr(r.From), x.iface(r.Iface)
			}
			sec.Firewall = &fw
		}
		sec.Volumes = slices.Clone(sec.Volumes)
		for i := range sec.Volumes {
			v := &sec.Volumes[i]
			v.Device, v.Mounts = x.path(v.Device), x.paths(v.Mounts)
		}
		sec.Swaps = slices.Clone(sec.Swaps)
		for i := range sec.Swaps {
			sec.Swaps[i].Name = x.path(sec.Swaps[i].Name)
		}
		c.Security = &sec
	}
	c.Unavailable = slices.Clone(c.Unavailable)
	for i := range c.Unavailable {
		c.Unavailable[i].Message = x.path(c.Unavailable[i].Message)
	}
	return &c
}

// System devuelve una copia del inventario sin hostname, series de los
// monitores ni nombres y direcciones Bluetooth o de pactl. Es también lo
// que se guarda en el caché del inventario.
func System(s *model.System) *model.System {
	if s == nil {
		return nil
	}
	y := *s
	y.Hostname = ""
	y.Monitors = slices.Clone(y.Monitors)
	for i := range y.Monitors {
		y.Monitors[i].Serial = ""
	}
	y.Bluetooth = slices.Clone(y.Bluetooth)
	for i := range y.Bluetooth {
		y.Bluetooth[i].Name, y.Bluetooth[i].Address = "", ""
	}
	if y.Sound != nil {
		so := *y.Sound
		so.Outputs, so.Inputs = nodes(so.Outputs), nodes(so.Inputs)
		y.Sound = &so
	}
	return &y
}

func nodes(ns []model.AudioNode) []model.AudioNode {
	ns = slices.Clone(ns)
	for i := range ns {
		ns[i].Name = ""
	}
	return ns
}

// humanUID: un usuario de persona (no root ni uno de sistema ni nobody).
func humanUID(uid uint32) bool { return uid >= 1000 && uid != 65534 }

// path cambia la carpeta del usuario por ~, su nombre en /run/media y
// /media por ~, y los UUIDs y las MACs por …. Sirve también para textos
// (mensajes).
func (x *scrub) path(p string) string {
	if p == "" {
		return p
	}
	if x.home != "" && x.home != "/" {
		p = replaceDir(p, x.home, "~")
	}
	if x.user != "" {
		for _, m := range []string{"/run/media/", "/media/"} {
			p = replaceDir(p, m+x.user, m+"~")
		}
	}
	return macRe.ReplaceAllString(uuidRe.ReplaceAllString(p, "…"), "…")
}

// replaceDir cambia dir por to donde aparece como carpeta entera (seguida
// de /, del final o de algo que no es parte de un nombre).
func replaceDir(s, dir, to string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, dir)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		end := i + len(dir)
		b.WriteString(s[:i])
		if end == len(s) || !nameByte(s[end]) {
			b.WriteString(to)
		} else {
			b.WriteString(dir)
		}
		s = s[end:]
	}
}

func nameByte(c byte) bool {
	return c == '_' || c == '-' || c == '.' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func (x *scrub) paths(ps []string) []string {
	if ps == nil {
		return nil
	}
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = x.path(p)
	}
	return out
}

// iface cambia un nombre de interfaz hecho con la MAC (wlx0123456789ab)
// por el prefijo y un número, el mismo en toda la muestra.
func (x *scrub) iface(n string) string {
	m := macIfaceRe.FindStringSubmatch(n)
	if m == nil {
		return n
	}
	if r, ok := x.ifaces[n]; ok {
		return r
	}
	r := m[1] + strconv.Itoa(len(x.ifaces)+1)
	x.ifaces[n] = r
	return r
}

// addr deja una dirección (o red: 192.168.1.0/24) solo si no identifica:
// IPv4 privadas, loopback, link-local o "cualquiera"; de IPv6, solo
// loopback, "cualquiera" y multicast (las globales llevan el prefijo del
// proveedor, las ULA el del sitio y las link-local, a veces, la MAC). Lo
// que no es una dirección (any, un nombre) queda.
func (x *scrub) addr(a string) string {
	if a == "" {
		return a
	}
	s, _, _ := strings.Cut(a, "/")
	s, _, _ = strings.Cut(s, "%")
	ip, err := netip.ParseAddr(s)
	if err != nil {
		return a
	}
	ip = ip.Unmap()
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() {
		return a
	}
	if ip.Is4() && (ip.IsPrivate() || ip.IsLinkLocalUnicast()) {
		return a
	}
	return ""
}

// froms limpia los orígenes permitidos de un puerto ("a, b, c"): quita
// los que identifican.
func (x *scrub) froms(f string) string {
	if f == "" {
		return f
	}
	var out []string
	for _, a := range strings.Split(f, ", ") {
		if a = x.addr(a); a != "" {
			out = append(out, a)
		}
	}
	return strings.Join(out, ", ")
}

func (x *scrub) addrs(as []string) []string {
	if as == nil {
		return nil
	}
	out := []string{}
	for _, a := range as {
		if a = x.addr(a); a != "" {
			out = append(out, a)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
