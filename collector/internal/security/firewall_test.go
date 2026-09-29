package security

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

func TestParseRules(t *testing.T) {
	data := strings.Join([]string{
		"### tuple ### allow tcp 1714:1764 0.0.0.0/0 any 0.0.0.0/0 in comment=4b444520436f6e6e656374",
		"-A ufw-user-input -p tcp -m multiport --dports 1714:1764 -j ACCEPT",
		"### tuple ### allow udp 53 172.17.0.1 any 172.16.0.0/12 in comment=616c6c6f772d646f636b65722d646e73",
		"### tuple ### allow tcp 22 0.0.0.0/0 any 0.0.0.0/0 OpenSSH - in",
		"### tuple ### allow_log tcp 80,443 0.0.0.0/0 any 0.0.0.0/0 in",
		"### tuple ### limit tcp 2222 0.0.0.0/0 any 203.0.113.0/24 in_enp5s0",
		"### tuple ### deny tcp 25 0.0.0.0/0 any 0.0.0.0/0 out",
		"### tuple ### route:allow tcp 80 0.0.0.0/0 any 0.0.0.0/0 in",
		"### tuple ### reject udp 69 0.0.0.0/0 any 0.0.0.0/0 in comment=zz-not-hex",
		"### tuple ### allow tcp",
	}, "\n")
	want := []model.FwRule{
		{Action: "allow", Proto: "tcp", Port: "1714:1764", To: "0.0.0.0/0", From: "0.0.0.0/0", Comment: "KDE Connect"},
		{Action: "allow", Proto: "udp", Port: "53", To: "172.17.0.1", From: "172.16.0.0/12", Comment: "allow-docker-dns"},
		{Action: "allow", Proto: "tcp", Port: "22", To: "0.0.0.0/0", From: "0.0.0.0/0", App: "OpenSSH"},
		{Action: "allow", Proto: "tcp", Port: "80,443", To: "0.0.0.0/0", From: "0.0.0.0/0"},
		{Action: "limit", Proto: "tcp", Port: "2222", To: "0.0.0.0/0", From: "203.0.113.0/24", Iface: "enp5s0"},
		{Action: "reject", Proto: "udp", Port: "69", To: "0.0.0.0/0", From: "0.0.0.0/0", Comment: "zz-not-hex"},
	}
	if got := ParseRules(data, false); !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	// ufw escapa los espacios del nombre de un perfil de app como %20.
	if app := ParseRules("### tuple ### allow tcp 1714:1764 0.0.0.0/0 any 0.0.0.0/0 KDE%20Connect - in", false); len(app) != 1 || app[0].App != "KDE Connect" {
		t.Errorf("app = %+v", app)
	}
	if app := ParseRules("### tuple ### allow tcp 80 0.0.0.0/0 any 0.0.0.0/0 Bad%zzName - in", false); len(app) != 1 || app[0].App != "Bad%zzName" {
		t.Errorf("bad escape = %+v", app)
	}
	if v6 := ParseRules("### tuple ### allow tcp 1716 ::/0 any ::/0 in", true); len(v6) != 1 || !v6[0].V6 || v6[0].To != "::/0" {
		t.Errorf("v6 = %+v", v6)
	}
}

func TestShellVars(t *testing.T) {
	got := shellVars("# c\nENABLED=\"yes\"\nIPV6='no'\nDEFAULT_INPUT_POLICY=REJECT # trailing\n  SPACED = x \nnoequals\n")
	want := map[string]string{"ENABLED": "yes", "IPV6": "no", "DEFAULT_INPUT_POLICY": "REJECT", "SPACED": "x"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("vars = %v", got)
	}
}

func TestFirewallState(t *testing.T) {
	on := ufwConfig{installed: true, enabled: true, ipv6: true, policy: "DROP", rules: []model.FwRule{{Action: "allow"}}}
	accept := on
	accept.policy = "ACCEPT"
	disabled := on
	disabled.enabled = false
	active := map[string]string{"ufw.service": "active"}
	for _, c := range []struct {
		name        string
		c           ufwConfig
		units       map[string]string
		tool, state string
		active      bool
		rules       int
	}{
		{"on", on, active, "ufw", "on", true, 1},
		{"on, systemd1 did not answer", on, nil, "ufw", "on", true, 1},
		{"policy ACCEPT", accept, active, "ufw", "on", false, 1},
		{"not loaded", on, map[string]string{"ufw.service": "inactive"}, "ufw", "not_loaded", false, 1},
		{"not loaded, firewalld runs", on, map[string]string{"ufw.service": "inactive", "firewalld.service": "active"}, "firewalld", "running", true, 0},
		{"disabled", disabled, active, "ufw", "off", false, 1},
		{"nftables", ufwConfig{}, map[string]string{"nftables.service": "active"}, "nftables", "running", true, 0},
		{"iptables", ufwConfig{}, map[string]string{"iptables.service": "active"}, "iptables", "running", true, 0},
		{"ip6tables", ufwConfig{}, map[string]string{"ip6tables.service": "active"}, "iptables", "running", true, 0},
		{"ufw disabled, iptables runs", disabled, map[string]string{"ufw.service": "inactive", "iptables.service": "active"}, "iptables", "running", true, 0},
		{"nothing", ufwConfig{}, map[string]string{}, "", "none", false, 0},
	} {
		fw := firewallState(c.c, c.units)
		if fw.Tool != c.tool || fw.State != c.state || fw.Active != c.active || len(fw.Rules) != c.rules {
			t.Errorf("%s: %+v", c.name, fw)
		}
	}
}

// Los archivos de ufw se releen solo si cambian; el estado de la unidad se
// mira en cada lectura.
func TestFirewallReaderRereadsOnlyOnChange(t *testing.T) {
	env := source.FixtureCopy(t, "threadripper")
	var fr FirewallReader
	fw := fr.Read(env, map[string]string{"ufw.service": "active"})
	if fw.Tool != "ufw" || fw.State != "on" || !fw.Active || fw.Policy != "DROP" || !fw.IPv6 || len(fw.Rules) != 11 {
		t.Fatalf("firewall = %+v", fw)
	}
	if fw := fr.Read(env, map[string]string{"ufw.service": "inactive"}); fw.State != "not_loaded" || fr.Parses() != 1 {
		t.Fatalf("unit stopped: %+v, parses %d", fw, fr.Parses())
	}
	conf := filepath.Join(env.FS.Root, "etc/ufw/ufw.conf")
	if err := os.WriteFile(conf, []byte("ENABLED=no\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(conf, later, later); err != nil {
		t.Fatal(err)
	}
	if fw := fr.Read(env, map[string]string{"ufw.service": "active"}); fw.State != "off" || fr.Parses() != 2 {
		t.Fatalf("disabled: %+v, parses %d", fw, fr.Parses())
	}
	// Cambiar un archivo de reglas (no ufw.conf) también dispara una relectura.
	rules := filepath.Join(env.FS.Root, "etc/ufw/user.rules")
	if err := os.WriteFile(rules, []byte("*filter\n:ufw-user-input - [0:0]\n### RULES ###\n### END RULES ###\nCOMMIT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	evenLater := later.Add(time.Minute)
	if err := os.Chtimes(rules, evenLater, evenLater); err != nil {
		t.Fatal(err)
	}
	if fw := fr.Read(env, map[string]string{"ufw.service": "active"}); fr.Parses() != 3 || len(fw.Rules) != 4 {
		t.Fatalf("user.rules vaciado: %+v, parses %d", fw, fr.Parses())
	}
}

// ufw-init (ufw-init-functions) solo activa ENABLED/IPV6 con "yes" o "YES"
// exactos: ni EqualFold ni "Yes" cuentan.
func TestUFWEnabledExactCase(t *testing.T) {
	env := source.FixtureCopy(t, "threadripper")
	conf := filepath.Join(env.FS.Root, "etc/ufw/ufw.conf")
	if err := os.WriteFile(conf, []byte("ENABLED=Yes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var fr FirewallReader
	if fw := fr.Read(env, map[string]string{"ufw.service": "active"}); fw.State != "off" {
		t.Fatalf("ENABLED=Yes: %+v", fw)
	}
}

// user.rules o user6.rules pueden existir sin poder leerse (permisos): ahí
// no se sabe qué deja pasar ufw, y el veredicto de cada puerto es
// "unknown", no "blocked".
func TestFirewallReaderUnreadableRules(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignora los permisos de archivo")
	}
	env := source.FixtureCopy(t, "threadripper")
	rules := filepath.Join(env.FS.Root, "etc/ufw/user.rules")
	if err := os.Chmod(rules, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(rules, 0o644)
	var fr FirewallReader
	fw := fr.Read(env, map[string]string{"ufw.service": "active"})
	if fw.Tool != "ufw" || fw.State != "on" || !fw.RulesUnknown {
		t.Fatalf("firewall = %+v", fw)
	}
	ls := []model.Listener{listener("udp", "0.0.0.0", 53317)}
	ApplyFirewall(ls, fw)
	if ls[0].Firewall != "unknown" {
		t.Errorf("verdict = %+v", ls[0])
	}
	// Arreglar los permisos (chmod no cambia el mtime ni el tamaño) se nota
	// en la lectura siguiente: mientras estén ilegibles se reintenta.
	if err := os.Chmod(rules, 0o644); err != nil {
		t.Fatal(err)
	}
	if fw := fr.Read(env, map[string]string{"ufw.service": "active"}); fw.RulesUnknown || len(fw.Rules) != 11 {
		t.Errorf("after chmod: %+v", fw)
	}
}

// Las excepciones fijas de before.rules: por familia y solo UDP; con
// IPV6=no las de v6 no abren nada (todo IPv6 queda bloqueado); una regla
// del usuario con un origen puntual se suma a la excepción, no la pisa.
func TestBeforeRulesExceptions(t *testing.T) {
	fw := func(ipv6 bool, rules ...model.FwRule) *model.Firewall {
		return &model.Firewall{Tool: "ufw", State: "on", Active: true, Policy: "DROP", IPv6: ipv6, Rules: rules}
	}
	v6l := func(proto string, port int) model.Listener {
		l := listener(proto, "::", port)
		return l
	}
	lan := model.FwRule{Action: "allow", Proto: "udp", Port: "5353", To: "0.0.0.0/0", From: "192.168.1.0/24"}
	for _, c := range []struct {
		name string
		fw   *model.Firewall
		l    model.Listener
		want [2]string
	}{
		{"udp 68 v4", fw(true), listener("udp", "0.0.0.0", 68), [2]string{"limited", "DHCP servers"}},
		{"tcp 68 is not DHCP", fw(true), listener("tcp", "0.0.0.0", 68), [2]string{"blocked", ""}},
		{"udp 546 v6", fw(true), v6l("udp", 546), [2]string{"limited", "DHCPv6 servers"}},
		{"udp 546 v6, IPV6=no", fw(false), v6l("udp", 546), [2]string{"blocked", ""}},
		{"udp 5353 on ::, IPV6=no keeps the v4 exception", fw(false), v6l("udp", 5353), [2]string{"limited", "mDNS (multicast)"}},
		{"udp 1900 v6, IPV6=no, v4 too", fw(false), v6l("udp", 1900), [2]string{"limited", "SSDP (multicast)"}},
		{"user source adds to the exception", fw(true, lan), listener("udp", "0.0.0.0", 5353), [2]string{"limited", "192.168.1.0/24, mDNS (multicast)"}},
	} {
		ls := []model.Listener{c.l}
		ApplyFirewall(ls, c.fw)
		if got := verdicts(ls)[0]; got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func listener(proto, addr string, port int) model.Listener {
	scope := "all"
	if addr != "0.0.0.0" && addr != "::" {
		scope = "local"
	}
	return model.Listener{Proto: proto, Addr: addr, Port: port, Scope: scope, Owners: []model.ListenerOwner{}}
}

func verdicts(ls []model.Listener) [][2]string {
	var out [][2]string
	for _, l := range ls {
		out = append(out, [2]string{l.Firewall, l.From})
	}
	return out
}

func TestApplyFirewall(t *testing.T) {
	var fr FirewallReader
	fw := fr.Read(source.Fixture(t, "threadripper"), map[string]string{"ufw.service": "active"})
	ls := func() []model.Listener {
		return []model.Listener{
			listener("tcp", "::", 1716), listener("udp", "::", 1716), listener("udp", "0.0.0.0", 5353),
			listener("tcp", "0.0.0.0", 2283), listener("udp", "0.0.0.0", 53), listener("tcp", "127.0.0.1", 631),
			listener("tcp", "::", 2283),
		}
	}
	got := ls()
	ApplyFirewall(got, fw)
	want := [][2]string{{"open", ""}, {"open", ""}, {"limited", "mDNS (multicast)"}, {"limited", "192.168.100.0/24"},
		{"limited", "172.16.0.0/12 → 172.17.0.1, 192.168.0.0/16 → 172.17.0.1"}, {"", ""}, {"limited", "192.168.100.0/24"}}
	if !reflect.DeepEqual(verdicts(got), want) {
		t.Fatalf("ufw on: %v", verdicts(got))
	}
	accept := *fw
	accept.Active = false
	got = ls()
	ApplyFirewall(got, &accept)
	if v := verdicts(got); v[0][0] != "none" || v[2][0] != "none" || v[5][0] != "" {
		t.Errorf("policy ACCEPT: %v", v)
	}
	// ufw con IPV6 != yes: ufw-init carga ip6tables con INPUT DROP y solo
	// acepta loopback (ufw-init-functions), así que lo que atiende en IPv6
	// queda blocked; en "::" gana lo más expuesto entre IPv6 y IPv4.
	v4only := *fw
	v4only.IPv6 = false
	got = ls()
	ApplyFirewall(got, &v4only)
	if v := verdicts(got); v[0][0] != "open" || v[2][0] != "limited" || v[3][0] != "limited" {
		t.Errorf("IPV6=no: %v", v)
	}
	got = ls()
	ApplyFirewall(got, &model.Firewall{Tool: "firewalld", State: "running", Active: true})
	if v := verdicts(got); v[0][0] != "unknown" || v[5][0] != "" {
		t.Errorf("firewalld: %v", v)
	}
	got = ls()
	ApplyFirewall(got, nil)
	for _, v := range verdicts(got) {
		if v != [2]string{"", ""} {
			t.Errorf("without firewall data: %v", v)
		}
	}
}

// El orden de las reglas manda, como en ufw.
func TestApplyFirewallOrder(t *testing.T) {
	rule := func(action, proto, port, from, iface string) model.FwRule {
		return model.FwRule{Action: action, Proto: proto, Port: port, To: "0.0.0.0/0", From: from, Iface: iface}
	}
	for _, c := range []struct {
		name  string
		rules []model.FwRule
		l     model.Listener
		want  [2]string
	}{
		{"deny before allow", []model.FwRule{rule("deny", "tcp", "8080", "0.0.0.0/0", ""), rule("allow", "tcp", "8080", "0.0.0.0/0", "")},
			listener("tcp", "0.0.0.0", 8080), [2]string{"blocked", ""}},
		{"sources add up", []model.FwRule{rule("allow", "tcp", "8080", "10.0.0.0/8", ""), rule("allow", "tcp", "8080", "192.168.0.0/16", "")},
			listener("tcp", "0.0.0.0", 8080), [2]string{"limited", "10.0.0.0/8, 192.168.0.0/16"}},
		{"deny from one source does not stop", []model.FwRule{rule("deny", "tcp", "8080", "203.0.113.9", ""), rule("allow", "tcp", "8080", "0.0.0.0/0", "")},
			listener("tcp", "0.0.0.0", 8080), [2]string{"open", ""}},
		{"one interface", []model.FwRule{rule("allow", "tcp", "9000", "0.0.0.0/0", "enp5s0")},
			listener("tcp", "0.0.0.0", 9000), [2]string{"limited", "enp5s0"}},
		{"specific destination, any source", []model.FwRule{
			{Action: "allow", Proto: "tcp", Port: "8080", To: "172.17.0.5", From: "0.0.0.0/0"},
		}, listener("tcp", "0.0.0.0", 8080), [2]string{"limited", "anywhere → 172.17.0.5"}},
		{"deny with a specific destination does not stop", []model.FwRule{
			{Action: "deny", Proto: "tcp", Port: "8080", To: "172.17.0.5", From: "0.0.0.0/0"},
			rule("allow", "tcp", "8080", "0.0.0.0/0", ""),
		}, listener("tcp", "0.0.0.0", 8080), [2]string{"open", ""}},
		{"port list and any protocol", []model.FwRule{rule("allow", "any", "80,443", "0.0.0.0/0", "")},
			listener("udp", "0.0.0.0", 443), [2]string{"open", ""}},
		{"limit counts as allow", []model.FwRule{rule("limit", "tcp", "22", "0.0.0.0/0", "")},
			listener("tcp", "0.0.0.0", 22), [2]string{"open", ""}},
		{"range edges", []model.FwRule{rule("allow", "tcp", "1714:1764", "0.0.0.0/0", "")},
			listener("tcp", "0.0.0.0", 1765), [2]string{"blocked", ""}},
		{"ipv6 deny from one source does not stop", []model.FwRule{
			{Action: "deny", Proto: "tcp", Port: "9092", To: "::/0", From: "2001:db8::1", V6: true},
			{Action: "allow", Proto: "tcp", Port: "9092", To: "::/0", From: "::/0", V6: true},
		}, listener("tcp", "::", 9092), [2]string{"open", ""}},
	} {
		ls := []model.Listener{c.l}
		ApplyFirewall(ls, &model.Firewall{Tool: "ufw", State: "on", Active: true, Policy: "DROP", IPv6: true, Rules: c.rules})
		if got := verdicts(ls)[0]; got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
