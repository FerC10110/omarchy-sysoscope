package fixture

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"regexp"
	"strings"
)

// Anonymizer reemplaza datos que identifican una máquina o a su dueño por
// valores de documentación, de forma consistente (el mismo original da
// siempre el mismo reemplazo).
type Anonymizer struct {
	home, hostname string
	words          *regexp.Regexp // usuario y hostname como palabras
	names          map[string]string
	macs           map[string]string
	serials        map[string]string
	serialOrder    []string
	uuids          map[string]string
	ssids          map[string]string
	ip4, ip6       map[string]string
	ll4            map[string]string // link-local IPv4 (169.254/16) → 169.254.0.N
	ll6            map[string]string // link-local IPv6 de /proc/net/* → fe80::N
	users, groups  map[string]string // otros usuarios/grupos de ALLOW_USERS y ALLOW_GROUPS
	progs          map[string]string // programas y apps de la salud (journal, volcados, OOM)
	ifaces         map[string]string // interfaces con el nombre de su MAC (enx/wlx/wwx<mac>)
}

func NewAnonymizer(user, home, hostname string) *Anonymizer {
	a := &Anonymizer{home: home, hostname: hostname, macs: map[string]string{}, serials: map[string]string{},
		uuids: map[string]string{}, ssids: map[string]string{}, ip4: map[string]string{}, ip6: map[string]string{},
		ll4: map[string]string{}, ll6: map[string]string{}, names: map[string]string{}, users: map[string]string{},
		groups: map[string]string{}, progs: map[string]string{}, ifaces: map[string]string{}}
	var alts []string
	for _, w := range []string{hostname, user} {
		if len(w) >= 3 && w != "root" && w != "user" && w != "host" {
			alts = append(alts, regexp.QuoteMeta(w))
			// Un nombre de dm de LVM escapa cada guion literal doblándolo
			// (host-name -> host--name, ver dm/name): si el hostname o el
			// usuario tienen un guion, también hay que reconocerlos así.
			if strings.Contains(w, "-") {
				alts = append(alts, regexp.QuoteMeta(strings.ReplaceAll(w, "-", "--")))
			}
		}
	}
	if len(alts) > 0 {
		a.words = regexp.MustCompile(`\b(` + strings.Join(alts, "|") + `)\b`)
	}
	return a
}

// Name reemplaza un nombre puesto por el dueño (p. ej. de un dispositivo
// Bluetooth) por device<N>.
func (a *Anonymizer) Name(s string) string {
	if r, ok := a.names[s]; ok {
		return r
	}
	r := fmt.Sprintf("device%d", len(a.names)+1)
	a.names[s] = r
	return r
}

// User reemplaza un usuario de ALLOW_USERS (snapper) que no es quien corre
// la captura (ese ya queda "user", vía Text) por userN, estable en toda la
// captura.
func (a *Anonymizer) User(s string) string {
	if r, ok := a.users[s]; ok {
		return r
	}
	r := fmt.Sprintf("user%d", len(a.users)+1)
	a.users[s] = r
	return r
}

// Group reemplaza un grupo de ALLOW_GROUPS (snapper) por groupN, estable en
// toda la captura.
func (a *Anonymizer) Group(s string) string {
	if r, ok := a.groups[s]; ok {
		return r
	}
	r := fmt.Sprintf("group%d", len(a.groups)+1)
	a.groups[s] = r
	return r
}

// Prog reemplaza un nombre de programa o de app de la salud del sistema
// (SYSLOG_IDENTIFIER/_COMM del journal, el exe de un volcado, o quien mató
// un proceso por falta de memoria) por progN, estable en toda la captura:
// el mismo nombre da siempre el mismo progN. "kernel" (la fuente del
// journal, no un programa) y la cadena vacía no cambian.
func (a *Anonymizer) Prog(s string) string {
	if s == "" || s == "kernel" {
		return s
	}
	if r, ok := a.progs[s]; ok {
		return r
	}
	r := fmt.Sprintf("prog%d", len(a.progs)+1)
	a.progs[s] = r
	return r
}

// Serial registra un número de serie para reemplazarlo en todos los textos.
func (a *Anonymizer) Serial(s string) {
	s = strings.TrimSpace(s)
	if len(s) < 4 {
		return
	}
	if _, ok := a.serials[s]; ok {
		return
	}
	a.serials[s] = fmt.Sprintf("SERIAL%d", len(a.serials)+1)
	a.serialOrder = append(a.serialOrder, s)
}

var (
	macRe  = regexp.MustCompile(`(?i)\b[0-9a-f]{2}(:[0-9a-f]{2}){5}\b`)
	uuidRe = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	ssidRe = regexp.MustCompile(`(?m)^(\s*SSID: )(.+)$`)
	nonAln = regexp.MustCompile(`[^A-Za-z0-9]`)
	// ifaceRe es una interfaz nombrada con su MAC (en/wl/ww + x + 12 hex, el
	// esquema de udev ID_NET_NAME_MAC): la MAC no sobrevive a Text (macRe
	// solo entiende hex separado por ":"), así que se trata aparte. El grupo
	// 1 (arranque de texto, o un carácter que no es letra ni dígito) no es
	// parte del nombre: RE2 no tiene "lookbehind", así que se lo captura y
	// se lo vuelve a escribir tal cual.
	ifaceRe = regexp.MustCompile(`(^|[^A-Za-z0-9])(en|wl|ww)x[0-9a-f]{12}\b`)
)

// Text anonimiza un texto: MACs, números de serie, UUIDs, SSIDs, la ruta
// del home, el usuario y el hostname. No toca IPs (ver IPs).
func (a *Anonymizer) Text(s string) string {
	if a.home != "" && a.home != "/" {
		s = strings.ReplaceAll(s, a.home, "/home/user")
	}
	if a.words != nil {
		s = a.words.ReplaceAllStringFunc(s, func(m string) string {
			// m puede venir en la forma lisa o, si apareció en un nombre de
			// dm de LVM, con cada guion doblado (ver NewAnonymizer): las dos
			// formas identifican el mismo hostname.
			if m == a.hostname || strings.ReplaceAll(m, "--", "-") == a.hostname {
				return "host"
			}
			return "user"
		})
	}
	s = macRe.ReplaceAllStringFunc(s, func(m string) string {
		m = strings.ToLower(m)
		if m == "00:00:00:00:00:00" || m == "ff:ff:ff:ff:ff:ff" {
			return m
		}
		if r, ok := a.macs[m]; ok {
			return r
		}
		r := fmt.Sprintf("02:00:00:00:00:%02x", len(a.macs)+1)
		a.macs[m] = r
		return r
	})
	for orig, repl := range a.macs { // rutas de bluez: dev_AA_BB_CC_DD_EE_FF
		s = strings.ReplaceAll(s, strings.ToUpper(strings.ReplaceAll(orig, ":", "_")), strings.ToUpper(strings.ReplaceAll(repl, ":", "_")))
	}
	s = ifaceRe.ReplaceAllStringFunc(s, func(m string) string {
		i := len(m) - 15 // "enx"/"wlx"/"wwx" + 12 hex
		lead, name := m[:i], m[i:]
		if r, ok := a.ifaces[name]; ok {
			return lead + r
		}
		r := fmt.Sprintf("%sx02%010x", name[:2], len(a.ifaces)+1)
		a.ifaces[name] = r
		return lead + r
	})
	if len(a.serialOrder) > 0 {
		// una sola pasada: un reemplazo nunca se vuelve a reemplazar
		var pairs []string
		for _, orig := range a.serialOrder {
			repl := a.serials[orig]
			pairs = append(pairs, orig, repl)
			if alt := nonAln.ReplaceAllString(orig, "_"); alt != orig {
				pairs = append(pairs, alt, repl)
			}
		}
		s = strings.NewReplacer(pairs...).Replace(s)
	}
	s = uuidRe.ReplaceAllStringFunc(s, func(m string) string {
		m = strings.ToLower(m)
		if r, ok := a.uuids[m]; ok {
			return r
		}
		r := fmt.Sprintf("00000000-0000-0000-0000-%012x", len(a.uuids)+1)
		a.uuids[m] = r
		return r
	})
	return ssidRe.ReplaceAllStringFunc(s, func(m string) string {
		p := ssidRe.FindStringSubmatch(m)
		r, ok := a.ssids[p[2]]
		if !ok {
			r = fmt.Sprintf("wifi%d", len(a.ssids)+1)
			a.ssids[p[2]] = r
		}
		return p[1] + r
	})
}

var domainRe = regexp.MustCompile(`(?m)^(search|domain)\s.*$`)

// IPs reemplaza las direcciones IP (salvo loopback) por rangos de
// documentación: 192.0.2.N y 2001:db8::N; los dominios de búsqueda de
// resolv.conf pasan a example.com.
func (a *Anonymizer) IPs(s string) string {
	s = domainRe.ReplaceAllString(s, "$1 example.com")
	lines := strings.SplitAfter(s, "\n")
	for i, l := range lines {
		f := strings.Fields(l)
		for _, w := range f {
			if r := a.ip(w); r != w {
				l = strings.Replace(l, w, r, 1)
			}
		}
		lines[i] = l
	}
	return strings.Join(lines, "")
}

func (a *Anonymizer) ip(w string) string {
	if h, zone, ok := strings.Cut(w, "%"); ok { // fe80::1%enp5s0
		if r := a.ip(h); r != h {
			return r + "%" + zone
		}
		return w
	}
	ip := net.ParseIP(w)
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
		return w
	}
	if ip.To4() != nil {
		if r, ok := a.ip4[w]; ok {
			return r
		}
		r := fmt.Sprintf("192.0.2.%d", len(a.ip4)+1)
		a.ip4[w] = r
		return r
	}
	if r, ok := a.ip6[w]; ok {
		return r
	}
	r := fmt.Sprintf("2001:db8::%x", len(a.ip6)+1)
	a.ip6[w] = r
	return r
}

// linkLocal4 busca o crea un pseudónimo estable dentro de 169.254.0.0/16
// (IPv4 link-local) para no perder el alcance de la dirección real.
func (a *Anonymizer) linkLocal4(k string) string {
	if r, ok := a.ll4[k]; ok {
		return r
	}
	r := fmt.Sprintf("169.254.0.%d", len(a.ll4)+1)
	a.ll4[k] = r
	return r
}

// linkLocal6 busca o crea un pseudónimo estable dentro de fe80::/10 (IPv6
// link-local) para no perder el alcance de la dirección real.
func (a *Anonymizer) linkLocal6(k string) string {
	if r, ok := a.ll6[k]; ok {
		return r
	}
	r := fmt.Sprintf("fe80::%x", len(a.ll6)+1)
	a.ll6[k] = r
	return r
}

// scopedIP anonimiza una IP ya decodificada conservando su alcance: loopback,
// sin dirección y multicast quedan igual (no pueden cambiar de alcance);
// link-local (169.254.0.0/16 o fe80::/10, venga mapeada en v6 o no) se
// mapea dentro del mismo rango (linkLocal4/linkLocal6); el resto usa el
// mapeo general de IPs (a.ip), en la familia de la dirección real — una v4
// mapeada en v6 (::ffff:a.b.c.d) usa el mismo 192.0.2.N que la misma
// dirección vista como v4 pura, no un 2001:db8::N.
func (a *Anonymizer) scopedIP(ip net.IP) string {
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() {
		return ip.String()
	}
	if v4 := ip.To4(); v4 != nil {
		if v4.IsLinkLocalUnicast() {
			return a.linkLocal4(v4.String())
		}
		return a.ip(v4.String())
	}
	if ip.IsLinkLocalUnicast() {
		return a.linkLocal6(ip.String())
	}
	return a.ip(ip.String())
}

// RouteHex anonimiza una IPv4 en el hex little-endian de /proc/net/route.
func (a *Anonymizer) RouteHex(h string) string {
	b, err := hex.DecodeString(h)
	if err != nil || len(b) != 4 || h == "00000000" {
		return h
	}
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, binary.LittleEndian.Uint32(b))
	r := net.ParseIP(a.ip(ip.String())).To4()
	if r == nil {
		return h
	}
	out := make([]byte, 4)
	binary.LittleEndian.PutUint32(out, binary.BigEndian.Uint32(r))
	return strings.ToUpper(hex.EncodeToString(out))
}

// EDID borra la serie numérica (bytes 12–15) y los descriptores de texto
// 0xFF (serie) y 0xFE (texto sin especificar: a veces lleva la serie), corta
// las extensiones (solo se lee el bloque base; DisplayID puede llevar una
// serie) y recalcula el checksum.
func EDID(e []byte) []byte {
	if len(e) < 128 {
		return e
	}
	e = append([]byte(nil), e[:128]...)
	e[126] = 0
	copy(e[12:16], []byte{0, 0, 0, 0})
	for off := 54; off+18 <= 126; off += 18 {
		if e[off] == 0 && e[off+1] == 0 && (e[off+3] == 0xff || e[off+3] == 0xfe) {
			copy(e[off+5:off+18], []byte("SERIAL\n      "))
		}
	}
	sum := byte(0)
	for _, b := range e[:127] {
		sum += b
	}
	e[127] = -sum
	return e
}

// NetHex anonimiza una dirección de /proc/net/{tcp,udp}[6] (hex en palabras
// de 32 bits little-endian): como IPs (ver scopedIP), salvo que loopback,
// sin dirección y multicast quedan (su alcance no puede cambiar), un
// link-local (169.254/16 o fe80::/10) conserva el rango y una v4 mapeada en
// v6 usa el mismo pseudónimo que la misma dirección vista como v4 pura.
func (a *Anonymizer) NetHex(h string) string {
	b, err := hex.DecodeString(h)
	if err != nil || (len(b) != 4 && len(b) != 16) {
		return h
	}
	ip := make(net.IP, len(b))
	for i := 0; i < len(b); i += 4 {
		ip[i], ip[i+1], ip[i+2], ip[i+3] = b[i+3], b[i+2], b[i+1], b[i]
	}
	r := net.ParseIP(a.scopedIP(ip))
	if len(b) == 4 {
		r = r.To4()
	} else {
		r = r.To16()
	}
	if r == nil {
		return h
	}
	out := make([]byte, len(b))
	for i := 0; i < len(b); i += 4 {
		out[i], out[i+1], out[i+2], out[i+3] = r[i+3], r[i+2], r[i+1], r[i]
	}
	return strings.ToUpper(hex.EncodeToString(out))
}
