package view

import (
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

// Lo de Seguridad que agrega la tanda 6: los puertos que escuchan con su
// alcance y lo que dice el firewall, las reglas de ufw y el cifrado de
// disco. Los avisos salen solo de lo que se lee también con la sección
// cerrada (Resumen y badge: entrada de attention).

// service es un servicio que escucha: protocolo y puerto. Un sshd en
// 0.0.0.0:22 y en [::]:22 (dual-stack) es uno solo, no dos.
type service struct {
	proto string
	port  int
}

// countServices cuenta los servicios distintos (protocolo, puerto) entre los
// puertos que cumplen keep — no las filas de la tabla.
func countServices(sec *model.Security, keep func(model.Listener) bool) int {
	seen := map[service]bool{}
	for _, x := range sec.Listening {
		if keep(x) {
			seen[service{x.Proto, x.Port}] = true
		}
	}
	return len(seen)
}

// exposedTCP cuenta los servicios TCP en todas las redes que nada filtra: el
// veredicto queda "none" tanto con el firewall apagado/sin cargar/ausente
// como con ufw activado pero con política de entrada ACCEPT (exposureWarnings
// distingue el texto según la causa); un ufw activo con IPV6=no bloquea
// igual el IPv6 (queda "blocked"), así que ese caso no cuenta.
func exposedTCP(sec *model.Security) int {
	return countServices(sec, func(x model.Listener) bool {
		return x.Proto == "tcp" && x.Scope == "all" && x.Firewall == "none"
	})
}

// openToAll cuenta los servicios en todas las redes que de verdad se
// alcanzan desde cualquier lado: veredicto open (una regla los deja pasar
// desde cualquier origen) o none (nada los filtra). limited queda afuera —
// solo entra tráfico de orígenes puntuales (DHCP, mDNS, una regla con un
// origen concreto), no "toda la red" — Task 7 ruling. Ver anyUnknown para
// cuando ni siquiera esto se puede afirmar.
func openToAll(sec *model.Security) int {
	return countServices(sec, func(x model.Listener) bool {
		return x.Scope == "all" && (x.Firewall == "open" || x.Firewall == "none")
	})
}

// anyUnknown: algún puerto en todas las redes con veredicto unknown (ufw con
// sus reglas ilegibles, o firewalld/nftables, cuyas reglas no se leen). El
// héroe muestra "?" en vez de un número que sería tranquilizador de más.
func anyUnknown(sec *model.Security) bool {
	for _, x := range sec.Listening {
		if x.Scope == "all" && x.Firewall == "unknown" {
			return true
		}
	}
	return false
}

// exposureWarnings: con ufw realmente sin filtrar (apagado o ausente) el
// texto es "Firewall off…"; con ufw activado pero en política ACCEPT el
// firewall corre pero deja pasar todo, así que el texto es otro ("Nothing
// filters incoming connections…") — Task 7 ruling. Con ufw activado pero
// ufw.service sin correr (not_loaded) no se sabe si las reglas están
// cargadas (otra cosa pudo cargarlas): el aviso dice solo eso.
func exposureWarnings(s *model.Snapshot, l string) []string {
	sec := s.Security
	if sec == nil {
		return nil
	}
	n := exposedTCP(sec)
	if n == 0 {
		return nil
	}
	fw := sec.Firewall
	if fw != nil && fw.Tool == "ufw" && fw.State == "not_loaded" {
		return []string{i18n.T(l, "ufw.service is not running; the firewall may not be loaded")}
	}
	if fw != nil && fw.State == "on" && fw.Policy == "ACCEPT" {
		if n == 1 {
			return []string{i18n.T(l, "Nothing filters incoming connections and 1 service listens on every network")}
		}
		return []string{i18n.F(l, "Nothing filters incoming connections and %d services listen on every network", n)}
	}
	if n == 1 {
		return []string{i18n.T(l, "Firewall off and 1 service listens on every network")}
	}
	return []string{i18n.F(l, "Firewall off and %d services listen on every network", n)}
}

// swapWarnings: una swap activa sin cifrar con / cifrado (la memoria se
// escribiría en claro). Un sistema entero sin cifrar no avisa.
func swapWarnings(s *model.Snapshot, l string) []string {
	sec := s.Security
	if sec == nil || !slices.ContainsFunc(sec.Volumes, func(v model.CryptVolume) bool { return v.Encrypted && slices.Contains(v.Mounts, "/") }) {
		return nil
	}
	var out []string
	for _, w := range sec.Swaps {
		if w.State == "plain" {
			out = append(out, i18n.F(l, "Swap is not encrypted (%s) while the system disk is", w.Name))
		}
	}
	return out
}

// securityWarnings son los avisos de Seguridad para el Resumen y su badge
// (entrada de attention).
func securityWarnings(s *model.Snapshot, l string) []Block {
	var out []Block
	for _, w := range append(exposureWarnings(s, l), swapWarnings(s, l)...) {
		out = append(out, Link(w, "warn", "security"))
	}
	return out
}

func exposureBlocks(s *model.Snapshot, l string) []Block {
	sec := s.Security
	if sec == nil {
		return nil
	}
	var bs []Block
	for _, w := range exposureWarnings(s, l) {
		bs = append(bs, Text(w, "warn"))
	}
	if sec.Firewall != nil {
		bs = append(bs, firewallLines(sec.Firewall, l)...)
	}
	if sec.Listening != nil {
		rs := make([]Row, 0, len(sec.Listening))
		for _, x := range sec.Listening {
			ap := addrPort(x)
			rs = append(rs, Row{ID: x.Proto + " " + ap, Cells: []string{protoText(x.Proto, l), ap, scopeText(x, l), ownersText(x.Owners, l), verdictText(x, sec.Firewall, l)}})
		}
		bs = append(bs, Table(i18n.T(l, "Exposed to the network"),
			[]string{i18n.T(l, "Protocol"), i18n.T(l, "Address"), i18n.T(l, "Scope"), i18n.T(l, "Program"), i18n.T(l, "Firewall")}, rs, false),
			Text(i18n.T(l, "Ports published by Docker containers skip these rules unless ufw-docker is set up"), ""))
	}
	bs = appendUnavailable(bs, s, "security.listening")
	if g := rulesGroup(sec.Firewall, l); g != nil {
		bs = append(bs, *g)
	}
	return bs
}

// firewallLines es el estado del firewall en una línea (dos si ufw tiene
// IPV6=no: la segunda es solo informativa, ya que ufw bloquea igual todo el
// IPv6 salvo loopback — Task 3 ruling; no hay nada que avisar). Con ufw
// activado pero sus reglas ilegibles (RulesUnknown), la línea no es la
// tranquilizadora "Firewall on (ufw)" en verde: no se sabe qué deja pasar.
func firewallLines(fw *model.Firewall, l string) []Block {
	switch fw.State {
	case "on":
		bs := []Block{Text(i18n.T(l, "Firewall on (ufw)"), "ok")}
		switch {
		case fw.Policy == "ACCEPT":
			bs = []Block{Text(i18n.T(l, "Firewall on (ufw), but it lets everything in (incoming policy ACCEPT)"), "warn")}
		case fw.RulesUnknown:
			bs = []Block{Text(i18n.T(l, "ufw is on but its rules could not be read"), "warn")}
		}
		if !fw.IPv6 {
			bs = append(bs, Text(i18n.T(l, "ufw blocks all IPv6 except loopback (IPV6=no in /etc/default/ufw)"), ""))
		}
		return bs
	case "not_loaded":
		return []Block{Text(i18n.T(l, "Firewall configured but not loaded (ufw.service is not running)"), "warn")}
	case "off":
		return []Block{Text(i18n.T(l, "Firewall off (ufw)"), "")}
	case "running":
		switch fw.Tool {
		case "firewalld":
			return []Block{Text(i18n.T(l, "firewalld is running (rules not read)"), "")}
		case "iptables":
			return []Block{Text(i18n.T(l, "iptables is running (rules not read)"), "")}
		}
		return []Block{Text(i18n.T(l, "nftables is running (rules not read)"), "")}
	}
	return []Block{Text(i18n.T(l, "No firewall"), "")}
}

func addrPort(x model.Listener) string {
	if a, err := netip.ParseAddr(x.Addr); err == nil {
		return netip.AddrPortFrom(a, uint16(x.Port)).String()
	}
	return x.Addr + ":" + strconv.Itoa(x.Port)
}

func protoText(p, l string) string {
	if p == "any" {
		return i18n.T(l, "any")
	}
	return strings.ToUpper(p)
}

func scopeText(x model.Listener, l string) string {
	where := x.Iface
	if where == "" {
		where = x.Addr
	}
	switch x.Scope {
	case "local":
		return i18n.T(l, "This computer only")
	case "all":
		return i18n.T(l, "Every network")
	case "link":
		return i18n.F(l, "Local link (%s)", where)
	case "multicast":
		return i18n.T(l, "Multicast (local network)")
	}
	return i18n.F(l, "Only %s", where)
}

func ownersText(os []model.ListenerOwner, l string) string {
	var out []string
	for _, o := range os {
		switch {
		case o.Own && o.Program != "":
			out = append(out, o.Program)
		case o.Own:
			out = append(out, i18n.T(l, "a program of yours"))
		default:
			who := o.User
			if who == "" {
				who = strconv.FormatUint(uint64(o.UID), 10)
			}
			if humanUID(o.UID) {
				out = append(out, i18n.F(l, "another user (%s)", who))
			} else {
				out = append(out, i18n.F(l, "system process (%s)", who))
			}
		}
	}
	return dash(strings.Join(out, ", "))
}

// humanUID: una cuenta de persona (1000..60000, el rango de login.defs);
// debajo, las de sistema, y arriba nobody (65534) y las de DynamicUser
// (61184–65519), que también son de servicios.
func humanUID(uid uint32) bool { return uid >= 1000 && uid <= 60000 }

// verdictText: el veredicto "none" es tanto "no hay firewall" (fw ausente)
// como "hay ufw pero no filtra" (apagado, sin cargar o política ACCEPT); con
// ufw presente el texto es "Not filtered", no el más rotundo "No firewall".
func verdictText(x model.Listener, fw *model.Firewall, l string) string {
	switch x.Firewall {
	case "open":
		return i18n.T(l, "Allowed from anywhere")
	case "limited":
		return i18n.F(l, "Allowed from %s", fromText(x.From, l))
	case "blocked":
		return i18n.T(l, "Blocked")
	case "none":
		if fw != nil && fw.Tool == "ufw" {
			return i18n.T(l, "Not filtered")
		}
		return i18n.T(l, "No firewall")
	case "unknown":
		return i18n.T(l, "Rules not read")
	}
	return "—"
}

// fromText traduce el origen de un veredicto limited: las excepciones fijas
// de before.rules ("DHCP servers", "mDNS (multicast)", "SSDP (multicast)",
// "DHCPv6 servers") y, dentro de un destino puntual ("anywhere → <dst>" o
// "<src> → <dst>"), la palabra "anywhere" (Task 3 ruling); las direcciones y
// nombres de interfaz quedan como están.
func fromText(from, l string) string {
	parts := strings.Split(from, ", ")
	for i, p := range parts {
		parts[i] = fromPart(p, l)
	}
	return strings.Join(parts, ", ")
}

func fromPart(p, l string) string {
	switch p {
	case "DHCP servers":
		return i18n.T(l, "DHCP servers")
	case "mDNS (multicast)":
		return i18n.T(l, "mDNS (multicast)")
	case "SSDP (multicast)":
		return i18n.T(l, "SSDP (multicast)")
	case "DHCPv6 servers":
		return i18n.T(l, "DHCPv6 servers")
	}
	if src, dst, ok := strings.Cut(p, " → "); ok {
		if src == "anywhere" {
			src = i18n.T(l, "anywhere")
		}
		return src + " → " + dst
	}
	return p
}

func anyNet(s string) bool { return s == "0.0.0.0/0" || s == "::/0" || s == "any" }

// rulesGroup son las reglas de entrada de ufw, plegables; las filas iguales
// de IPv4 e IPv6 se juntan.
func rulesGroup(fw *model.Firewall, l string) *Block {
	if fw == nil || len(fw.Rules) == 0 {
		return nil
	}
	var rs []Row
	seen := map[string]bool{}
	for _, r := range fw.Rules {
		port := r.Port
		if port == "any" {
			port = i18n.T(l, "any")
		}
		if r.App != "" {
			port = r.App + " (" + r.Port + ")"
		}
		if !anyNet(r.To) {
			port += " → " + r.To
		}
		from := r.From
		if anyNet(from) {
			from = i18n.T(l, "anywhere")
		}
		if r.Iface != "" {
			from = i18n.F(l, "%s on %s", from, r.Iface)
		}
		cells := []string{actionText(r.Action, l), port, protoText(r.Proto, l), from, dash(cleanComment(r.Comment))}
		k := strings.Join(cells, "\x00")
		if seen[k] {
			continue
		}
		seen[k] = true
		rs = append(rs, Row{ID: strconv.Itoa(len(rs)), Cells: cells})
	}
	g := Group(i18n.T(l, "Firewall rules"), Table(i18n.T(l, "Rules for incoming connections"),
		[]string{i18n.T(l, "Action"), i18n.T(l, "Port"), i18n.T(l, "Protocol"), i18n.T(l, "From"), i18n.T(l, "Comment")}, rs, false))
	return &g
}

// csiSeq es una secuencia de escape ANSI/CSI completa: ESC '[', los bytes de
// parámetro (dígitos, ';', '?', …), los intermedios si hay, y el byte final
// que la cierra (una letra: 'm' de color, 'H' de posición, etc.) — no solo
// el ESC, que dejaría el resto ("31m") como basura visible.
var csiSeq = regexp.MustCompile("\x1b\\[[0-?]*[ -/]*[@-~]")

// cleanComment sacas las secuencias ANSI/CSI y los caracteres de control y
// de formato Unicode (Cf, como el marcador de dirección RTL) de un
// comentario de ufw antes de mostrarlo: son parte del texto que el usuario
// escribió al crear la regla, no algo que sysoscope deba interpretar.
func cleanComment(s string) string {
	s = csiSeq.ReplaceAllString(s, "")
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
}

func actionText(a, l string) string {
	switch a {
	case "allow":
		return i18n.T(l, "Allow")
	case "deny":
		return i18n.T(l, "Deny")
	case "reject":
		return i18n.T(l, "Reject")
	case "limit":
		return i18n.T(l, "Limit")
	}
	return a
}

func encryptionBlocks(s *model.Snapshot, l string) []Block {
	sec := s.Security
	if sec == nil || (len(sec.Volumes) == 0 && len(sec.Swaps) == 0) {
		return nil
	}
	var bs []Block
	for _, w := range swapWarnings(s, l) {
		bs = append(bs, Text(w, "warn"))
	}
	var rs [][2]string
	for _, v := range sec.Volumes {
		rs = append(rs, [2]string{strings.Join(v.Mounts, ", "), cryptText(v.Encrypted, v.Type, v.On, l)})
	}
	for _, w := range sec.Swaps {
		val := i18n.T(l, "Unknown")
		switch w.State {
		case "memory":
			val = i18n.T(l, "In memory")
		case "encrypted":
			val = cryptText(true, w.Type, w.On, l)
		case "plain":
			val = i18n.T(l, "Not encrypted")
		}
		rs = append(rs, [2]string{i18n.F(l, "Swap %s", w.Name), val})
	}
	return append(bs, KV(i18n.T(l, "Disk encryption"), rs...))
}

func cryptText(enc bool, typ, on, l string) string {
	switch {
	case !enc:
		return i18n.T(l, "Not encrypted")
	case typ != "" && on != "":
		return i18n.F(l, "Encrypted (%s on %s)", typ, on)
	case typ != "":
		return i18n.F(l, "Encrypted (%s)", typ)
	}
	return i18n.T(l, "Encrypted")
}
