package security

import (
	"encoding/hex"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// Los archivos de ufw que se leen: legibles por todos (ufw status pide root
// y no se usa).
const (
	UFWConf    = "/etc/ufw/ufw.conf"
	UFWDefault = "/etc/default/ufw"
	UFWRules   = "/etc/ufw/user.rules"
	UFWRules6  = "/etc/ufw/user6.rules"
)

// UFWFiles son los archivos de ufw que se leen (y se miran por mtime).
var UFWFiles = []string{UFWConf, UFWDefault, UFWRules, UFWRules6}

type ufwConfig struct {
	installed, enabled, ipv6, unreadable bool
	policy                               string
	rules                                []model.FwRule
}

type stamp struct {
	mod, size int64
	mode      os.FileMode
	ok        bool
}

// FirewallReader lee el estado del firewall: los archivos de ufw solo si
// cambió su mtime, su tamaño o sus permisos (un stat por archivo) o si la
// vez anterior un archivo de reglas no se pudo leer (un chmod de la carpeta
// no cambia el stat del archivo), y el estado de las unidades en cada
// lectura (llega de la llamada a systemd1 del reloj).
type FirewallReader struct {
	stamps [4]stamp
	cfg    ufwConfig
	read   bool
	parses int
}

// Read arma el Firewall. units es el ActiveState de las unidades cargadas
// (system.Units); nil si systemd1 no respondió.
func (fr *FirewallReader) Read(env source.Env, units map[string]string) *model.Firewall {
	var st [4]stamp
	for i, p := range UFWFiles {
		if fi, err := os.Stat(env.FS.Real(p)); err == nil {
			st[i] = stamp{fi.ModTime().UnixNano(), fi.Size(), fi.Mode(), true}
		}
	}
	if !fr.read || st != fr.stamps || fr.cfg.unreadable {
		fr.cfg, fr.stamps, fr.read = readUFW(env), st, true
		fr.parses++
	}
	return firewallState(fr.cfg, units)
}

// Parses dice cuántas veces se leyeron los archivos de ufw.
func (fr *FirewallReader) Parses() int { return fr.parses }

func readUFW(env source.Env) ufwConfig {
	c := ufwConfig{policy: "DROP", ipv6: true} // los valores por defecto de ufw
	conf, err := env.FS.Read(UFWConf)
	if err != nil {
		return c
	}
	c.installed = true
	c.enabled = isYes(shellVars(conf)["ENABLED"])
	if d, err := env.FS.Read(UFWDefault); err == nil {
		v := shellVars(d)
		if p := strings.ToUpper(v["DEFAULT_INPUT_POLICY"]); p != "" {
			c.policy = p
		}
		if s, ok := v["IPV6"]; ok {
			c.ipv6 = isYes(s)
		}
	}
	// Un archivo de reglas ausente es "sin reglas" (ufw recién instalado); uno
	// que existe pero no se puede leer (permisos) deja los veredictos en
	// unknown: no se sabe qué deja pasar.
	for _, f := range []struct {
		path string
		v6   bool
	}{{UFWRules, false}, {UFWRules6, true}} {
		switch data, err := env.FS.Read(f.path); {
		case err == nil:
			c.rules = append(c.rules, ParseRules(data, f.v6)...)
		case !os.IsNotExist(err):
			c.unreadable = true
		}
	}
	return c
}

// isYes: ufw-init (ufw-init-functions) solo activa ENABLED/IPV6 con "yes" o
// "YES" exactos, no con cualquier mayúscula/minúscula ("Yes" no cuenta).
func isYes(s string) bool { return s == "yes" || s == "YES" }

// shellVars lee CLAVE=valor como un archivo de shell simple: comillas
// dobles o simples, # comentario (también al final, sin comillas),
// espacios alrededor.
func shellVars(s string) map[string]string {
	out := map[string]string{}
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		} else if i := strings.Index(v, " #"); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		out[strings.TrimSpace(k)] = v
	}
	return out
}

// ParseRules lee las líneas "### tuple ###" de user.rules o user6.rules:
// <acción> <proto> <puerto dst> <dst> <puerto src> <src> [<app dst> <app src>]
// <dirección>[ comment=<hex>]. Solo las de entrada (in, in_<interfaz>): las
// de salida y las de ruteo (route:…) no. La acción pierde el _log.
func ParseRules(data string, v6 bool) []model.FwRule {
	var out []model.FwRule
	for _, l := range strings.Split(data, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(l), "### tuple ###")
		if !ok {
			continue
		}
		comment := ""
		if i := strings.Index(rest, " comment="); i >= 0 {
			comment = decodeComment(strings.TrimSpace(rest[i+len(" comment="):]))
			rest = rest[:i]
		}
		f := strings.Fields(rest)
		if (len(f) != 7 && len(f) != 9) || strings.HasPrefix(f[0], "route:") {
			continue
		}
		dir, iface, _ := strings.Cut(f[len(f)-1], "_")
		if dir != "in" {
			continue
		}
		action, _, _ := strings.Cut(f[0], "_")
		r := model.FwRule{Action: action, Proto: f[1], Port: f[2], To: f[3], From: f[5], Iface: iface, V6: v6, Comment: comment}
		if len(f) == 9 && f[6] != "-" {
			r.App = unescapeApp(f[6])
		}
		out = append(out, r)
	}
	return out
}

// unescapeApp: ufw escribe el nombre de un perfil de app con los espacios
// como %20 ("KDE%20Connect"); cualquier %XX se decodifica. Un escape
// inválido deja el nombre como está.
func unescapeApp(s string) string {
	if u, err := url.PathUnescape(s); err == nil {
		return u
	}
	return s
}

// decodeComment: ufw guarda el comentario en hex. Si no es hex válido (o no
// es texto), queda como está.
func decodeComment(h string) string {
	if b, err := hex.DecodeString(h); err == nil && utf8.Valid(b) {
		return string(b)
	}
	return h
}

// firewallState decide qué firewall filtra, en este orden: ufw cargado,
// firewalld, nftables o iptables/ip6tables corriendo, ufw configurado sin cargar, ufw apagado,
// ninguno. Sin respuesta de systemd1 (units nil), ENABLED=yes cuenta como
// cargado.
func firewallState(c ufwConfig, units map[string]string) *model.Firewall {
	fw := &model.Firewall{}
	switch {
	case c.installed && c.enabled && (units == nil || units["ufw.service"] == "active"):
		fw.State, fw.Active = "on", c.policy != "ACCEPT"
	case units["firewalld.service"] == "active":
		return &model.Firewall{Tool: "firewalld", State: "running", Active: true}
	case units["nftables.service"] == "active":
		return &model.Firewall{Tool: "nftables", State: "running", Active: true}
	case units["iptables.service"] == "active" || units["ip6tables.service"] == "active":
		// Reglas cargadas por iptables-restore: como nftables, no se leen.
		return &model.Firewall{Tool: "iptables", State: "running", Active: true}
	case c.installed && c.enabled:
		fw.State = "not_loaded"
	case c.installed:
		fw.State = "off"
	default:
		return &model.Firewall{State: "none"}
	}
	fw.Tool, fw.Policy, fw.IPv6, fw.Rules, fw.RulesUnknown = "ufw", c.policy, c.ipv6, c.rules, c.unreadable
	return fw
}

// exposure ordena los veredictos de menos a más expuesto.
var exposure = map[string]int{"blocked": 0, "limited": 1, "open": 2, "none": 3}

// ApplyFirewall pone en cada puerto que atiende en todas las redes qué le
// pasa a una conexión que llega de afuera: open (una regla la deja pasar
// desde cualquier lado), limited (solo desde From), blocked (la política la
// descarta), none (ningún firewall la filtra) o unknown (firewalld,
// nftables, iptables, o ufw con sus reglas ilegibles: no se sabe qué filtran). Los
// demás puertos quedan sin veredicto.
func ApplyFirewall(ls []model.Listener, fw *model.Firewall) {
	for i := range ls {
		l := &ls[i]
		l.Firewall, l.From = "", ""
		if l.Scope != "all" || fw == nil {
			continue
		}
		l.Firewall, l.From = verdict(*l, fw)
	}
}

func verdict(l model.Listener, fw *model.Firewall) (string, string) {
	switch {
	case !fw.Active:
		return "none", ""
	case fw.Tool != "ufw" || fw.RulesUnknown:
		return "unknown", ""
	}
	v6 := strings.Contains(l.Addr, ":")
	fams := []bool{v6}
	if v6 {
		fams = append(fams, false) // "::" también atiende IPv4
	}
	best, from := "blocked", ""
	for _, fam6 := range fams {
		v, f := "blocked", ""
		switch {
		case fam6 && !fw.IPv6:
			// ufw-init (ufw-init-functions) con IPV6 != yes: cae a ip6tables
			// con INPUT DROP y solo acepta loopback; nada de IPv6 pasa.
		default:
			v, f = ruleVerdict(l, fw.Rules, fam6)
			v, f = withBeforeRules(l, fam6, v, f)
		}
		if exposure[v] > exposure[best] {
			best, from = v, f
		}
	}
	return best, from
}

// beforeRules son las excepciones fijas de before.rules/before6.rules: lo
// que ufw deja pasar antes de las reglas del usuario. Son legibles, pero su
// formato es el de iptables-restore: en vez de interpretarlo se modelan a
// mano las que trae ufw (esta máquina: DHCP, mDNS y SSDP multicast; antes
// hay solo loopback, established e ICMP, que no importan acá). Si el
// usuario las sacó, esto muestra de más, no de menos.
var beforeRules = []struct {
	v6   bool
	port int
	from string
}{
	{false, 68, "DHCP servers"},
	{false, 5353, "mDNS (multicast)"},
	{false, 1900, "SSDP (multicast)"},
	{true, 546, "DHCPv6 servers"},
	{true, 5353, "mDNS (multicast)"},
	{true, 1900, "SSDP (multicast)"},
}

// withBeforeRules aplica las excepciones de beforeRules: un puerto udp que
// coincide queda al menos "limited" con ese origen, sumado a los orígenes
// que ya dejaban pasar las reglas del usuario, salvo que estas ya lo dejen
// "open" (gana lo más expuesto). before.rules no distingue por scope, pero
// ApplyFirewall ya llama a esto solo para el scope "all".
func withBeforeRules(l model.Listener, v6 bool, v, f string) (string, string) {
	if v == "open" || l.Proto != "udp" {
		return v, f
	}
	for _, b := range beforeRules {
		if b.v6 != v6 || b.port != l.Port {
			continue
		}
		if v == "limited" && f != "" {
			if slices.Contains(strings.Split(f, ", "), b.from) {
				return v, f
			}
			return "limited", f + ", " + b.from
		}
		return "limited", b.from
	}
	return v, f
}

// ruleVerdict recorre las reglas de una familia en orden, como ufw: un
// allow o limit desde cualquier lado y a cualquier destino deja pasar; uno
// con un origen puntual, una interfaz o un destino puntual se junta como
// limited (destino puntual: "<origen> → <destino>", "anywhere" si el
// origen es cualquiera); un deny o reject sin condiciones (cualquier
// origen, cualquier destino, sin interfaz) corta el resto.
func ruleVerdict(l model.Listener, rules []model.FwRule, v6 bool) (string, string) {
	var froms []string
	for _, r := range rules {
		if r.V6 != v6 || (r.Proto != "any" && r.Proto != l.Proto) || !portMatch(r.Port, l.Port) {
			continue
		}
		if r.Action != "allow" && r.Action != "limit" {
			if r.Iface == "" && anyAddr(r.From) && anyAddr(r.To) {
				break
			}
			continue
		}
		if !anyAddr(r.To) {
			src := "anywhere"
			if !anyAddr(r.From) {
				src = r.From
			}
			label := src + " → " + r.To
			if !slices.Contains(froms, label) {
				froms = append(froms, label)
			}
			continue
		}
		src := r.From
		switch {
		case r.Iface == "" && anyAddr(src):
			return "open", ""
		case anyAddr(src):
			src = r.Iface
		}
		if !slices.Contains(froms, src) {
			froms = append(froms, src)
		}
	}
	if len(froms) > 0 {
		return "limited", strings.Join(froms, ", ")
	}
	return "blocked", ""
}

func anyAddr(s string) bool { return s == "0.0.0.0/0" || s == "::/0" || s == "any" }

// portMatch: "any", un puerto, un rango "a:b" o una lista de ellos con comas.
func portMatch(spec string, port int) bool {
	if spec == "any" {
		return true
	}
	for _, part := range strings.Split(spec, ",") {
		lo, hi, isRange := strings.Cut(part, ":")
		a, err1 := strconv.Atoi(lo)
		b, err2 := a, error(nil)
		if isRange {
			b, err2 = strconv.Atoi(hi)
		}
		if err1 == nil && err2 == nil && port >= a && port <= b {
			return true
		}
	}
	return false
}
