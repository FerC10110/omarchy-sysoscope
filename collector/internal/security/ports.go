// Package security lee lo de la sección Seguridad que no es la cámara ni el
// micrófono: los puertos que escuchan (/proc/net) y de quién son, el
// firewall (los archivos de ufw y el estado de sus unidades), qué deja
// pasar a cada puerto y el cifrado de disco (sysfs). Todo de solo lectura y
// sin root: nada de lo que está acá cambia el sistema.
package security

import (
	"cmp"
	"encoding/hex"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/maint"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// Socket es un socket que escucha, como lo da /proc/net.
type Socket struct {
	Proto string // tcp|udp
	Addr  netip.Addr
	Port  uint16
	UID   uint32
	Inode uint64
}

// netFiles son las tablas de sockets del kernel, legibles por cualquiera:
// primero las de TCP (en la máquina real se usa netlink en su lugar) y
// después las de UDP.
var netFiles = []struct{ path, proto string }{
	{"/proc/net/tcp", "tcp"}, {"/proc/net/tcp6", "tcp"}, {"/proc/net/udp", "udp"}, {"/proc/net/udp6", "udp"},
}

// udpFiles son las tablas de netFiles que se leen igual cuando TCP salió de
// netlink.
var udpFiles = netFiles[2:]

// PortRange es el rango de puertos efímeros (los de los clientes).
const PortRange = "/proc/sys/net/ipv4/ip_local_port_range"

// ReadSockets lee los sockets que escuchan: TCP en LISTEN y UDP sin
// destino, salvo los UDP en el rango de puertos efímeros (son clientes, no
// servicios). nil (con su unavailable) si no se pudo leer ninguna tabla.
// En la máquina real (sin FS.Root) TCP sale de netlink sock_diag (ver
// diag.go); los fixtures, y la máquina real si netlink falla, usan
// /proc/net/tcp{,6}.
func ReadSockets(env source.Env, r *collect.Report) []Socket {
	lo, hi := ephemeral(env)
	out := []Socket{}
	read := 0
	files := netFiles
	if env.FS.Root == "" {
		if socks, err := listenTCP(); err == nil {
			out = append(out, socks...)
			read++
			files = udpFiles
		}
	}
	for _, f := range files {
		data, err := env.FS.Read(f.path)
		if err != nil {
			continue
		}
		read++
		out = append(out, ParseNet(data, f.proto, lo, hi)...)
	}
	if read == 0 {
		r.Absent("security.listening")
		return nil
	}
	return out
}

func ephemeral(env source.Env) (uint16, uint16) {
	if s, err := env.FS.Read(PortRange); err == nil {
		if f := strings.Fields(s); len(f) == 2 {
			lo, e1 := strconv.ParseUint(f[0], 10, 16)
			hi, e2 := strconv.ParseUint(f[1], 10, 16)
			if e1 == nil && e2 == nil && lo <= hi {
				return uint16(lo), uint16(hi)
			}
		}
	}
	return 32768, 60999
}

// ParseNet interpreta una tabla de /proc/net (tcp, tcp6, udp o udp6): las
// columnas son sl, local, remota, estado, …, uid (7) e inode (9). Una línea
// que no se entiende se saltea.
func ParseNet(data, proto string, lo, hi uint16) []Socket {
	var out []Socket
	for _, l := range strings.Split(data, "\n") {
		f := strings.Fields(l)
		if len(f) < 10 || !strings.HasSuffix(f[0], ":") {
			continue
		}
		addr, port, ok := hostPort(f[1])
		if !ok {
			continue
		}
		switch proto {
		case "tcp":
			if f[3] != "0A" { // LISTEN
				continue
			}
		case "udp":
			raddr, rport, ok := hostPort(f[2])
			if !ok || f[3] != "07" || !raddr.IsUnspecified() || rport != 0 || (port >= lo && port <= hi) {
				continue
			}
		}
		uid, err1 := strconv.ParseUint(f[7], 10, 32)
		inode, err2 := strconv.ParseUint(f[9], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		out = append(out, Socket{Proto: proto, Addr: addr, Port: port, UID: uint32(uid), Inode: inode})
	}
	return out
}

// hostPort decodifica "0100007F:0277" o la forma de 32 dígitos de IPv6: la
// dirección va en palabras de 32 bits en el orden del procesador
// (little-endian en amd64 y arm64), el puerto en hex común.
func hostPort(s string) (netip.Addr, uint16, bool) {
	h, p, ok := strings.Cut(s, ":")
	if !ok {
		return netip.Addr{}, 0, false
	}
	port, err := strconv.ParseUint(p, 16, 16)
	if err != nil {
		return netip.Addr{}, 0, false
	}
	b, err := hex.DecodeString(h)
	if err != nil || (len(b) != 4 && len(b) != 16) {
		return netip.Addr{}, 0, false
	}
	for i := 0; i < len(b); i += 4 {
		b[i], b[i+1], b[i+2], b[i+3] = b[i+3], b[i+2], b[i+1], b[i]
	}
	a, _ := netip.AddrFromSlice(b)
	return a, uint16(port), true
}

// Users es uid → nombre de /etc/passwd: un envoltorio fino sobre
// maint.UserNames, el mismo parser que usa Mantenimiento (evita tener dos
// lectores de /etc/passwd).
func Users(env source.Env) map[uint32]string {
	return maint.UserNames(env)
}

// scopeRank ordena las filas: primero lo más expuesto.
var scopeRank = map[string]int{"all": 0, "addr": 1, "link": 2, "multicast": 3, "local": 4}

// Listening agrupa los sockets por protocolo, dirección y puerto (un mismo
// servicio en IPv4 e IPv6 queda en dos filas), les pone su alcance y sus
// dueños, y los ordena: primero lo que atiende en todas las redes. program
// da el programa de un socket propio (nil: no se buscan, Seguridad
// cerrada). nil si los sockets no se pudieron leer.
func Listening(socks []Socket, ifaces []model.Iface, users map[uint32]string, own uint32, program func(inode uint64) (string, bool)) []model.Listener {
	if socks == nil {
		return nil
	}
	type key struct {
		proto string
		addr  netip.Addr
		port  uint16
	}
	idx := map[key]int{}
	out := []model.Listener{}
	for _, s := range socks {
		a := s.Addr.Unmap()
		k := key{s.Proto, a, s.Port}
		i, ok := idx[k]
		if !ok {
			scope, iface := scopeOf(a, ifaces)
			out = append(out, model.Listener{Proto: s.Proto, Addr: a.String(), Port: int(s.Port), Scope: scope, Iface: iface,
				Owners: []model.ListenerOwner{}})
			i = len(out) - 1
			idx[k] = i
		}
		o := model.ListenerOwner{UID: s.UID, User: users[s.UID], Own: s.UID == own}
		if o.Own && program != nil {
			o.Program, _ = program(s.Inode)
		}
		out[i].Owners = addOwner(out[i].Owners, o)
	}
	slices.SortStableFunc(out, func(a, b model.Listener) int {
		return cmp.Or(cmp.Compare(scopeRank[a.Scope], scopeRank[b.Scope]), cmp.Compare(a.Proto, b.Proto),
			cmp.Compare(a.Port, b.Port), cmp.Compare(a.Addr, b.Addr))
	})
	return out
}

// addOwner suma un dueño sin repetir: del mismo uid, uno sin programa no
// agrega nada y uno con programa reemplaza al que no lo tenía.
func addOwner(os []model.ListenerOwner, o model.ListenerOwner) []model.ListenerOwner {
	for i, x := range os {
		if x.UID != o.UID {
			continue
		}
		if x.Program == o.Program || o.Program == "" {
			return os
		}
		if x.Program == "" {
			os[i] = o
			return os
		}
	}
	return append(os, o)
}

// scopeOf dice hasta dónde llega una dirección de escucha y, para una
// dirección puntual o link-local, qué interfaz la tiene.
func scopeOf(a netip.Addr, ifaces []model.Iface) (string, string) {
	switch {
	case a.IsLoopback():
		return "local", ""
	case a.IsUnspecified():
		return "all", ""
	case a.IsMulticast():
		return "multicast", ""
	case a.IsLinkLocalUnicast():
		return "link", ifaceOf(a, ifaces)
	}
	return "addr", ifaceOf(a, ifaces)
}

func ifaceOf(a netip.Addr, ifaces []model.Iface) string {
	for _, it := range ifaces {
		for _, c := range append(slices.Clone(it.IPv4), it.IPv6...) {
			if p, err := netip.ParsePrefix(c); err == nil && p.Addr() == a {
				return it.Name
			}
		}
	}
	return ""
}
