package view

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/engine"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// secSnap es el segundo tick de un engine con ese foco sobre el fixture.
func secSnap(t *testing.T, fixture, focus string) *model.Snapshot {
	t.Helper()
	e := engine.New(source.Fixture(t, fixture), engine.Options{Interval: 2 * time.Second, Focus: focus, Statfs: fakeStatfs})
	e.Tick()
	return e.Tick()
}

func statByLabel(h Block, label string) Block {
	for _, b := range h.Blocks {
		if b.Label == label {
			return b
		}
	}
	return Block{}
}

func indexOf(bs []Block, typ, title string) int {
	return slices.IndexFunc(bs, func(b Block) bool { return b.Type == typ && b.Title == title })
}

func TestSecurityViewThreadripper(t *testing.T) {
	s := secSnap(t, "threadripper", "security")
	v, err := Build("security", s, Options{Lang: "en"})
	if err != nil {
		t.Fatal(err)
	}
	rs := tableRows(findTable(v.Blocks, "Exposed to the network"))
	if len(rs) != 14 {
		t.Fatalf("%d rows", len(rs))
	}
	for i, want := range map[int][]string{
		0:  {"TCP", "[::]:1716", "Every network", "prog2", "Allowed from anywhere"},
		1:  {"UDP", "[::]:1716", "Every network", "prog2", "Allowed from anywhere"},
		2:  {"UDP", "0.0.0.0:5353", "Every network", "system process (avahi)", "Allowed from mDNS (multicast)"},
		4:  {"TCP", "172.17.0.1:53", "Only 172.17.0.1", "system process (systemd-resolve)", "—"},
		6:  {"UDP", "[fe80::1]:546", "Local link (fe80::1)", "system process (root)", "—"},
		7:  {"UDP", "224.0.0.251:5353", "Multicast (local network)", "prog3", "—"},
		10: {"TCP", "127.0.0.1:631", "This computer only", "system process (root)", "—"},
	} {
		if !reflect.DeepEqual(rs[i].Cells, want) {
			t.Errorf("row %d = %q, want %q", i, rs[i].Cells, want)
		}
	}
	if rs[0].ID != "tcp [::]:1716" {
		t.Errorf("row id = %q", rs[0].ID)
	}
	tx := texts(v)
	if tx["Firewall on (ufw)"] != "ok" {
		t.Errorf("texts = %v", tx)
	}
	if _, ok := tx["Ports published by Docker containers skip these rules unless ufw-docker is set up"]; !ok {
		t.Error("no Docker note")
	}
	if w := securityWarnings(s, "en"); len(w) != 0 {
		t.Errorf("warnings on a machine that needs nothing: %+v", w)
	}
	g := findGroup(v.Blocks, "Firewall rules")
	if len(g.Blocks) != 1 {
		t.Fatalf("rules group = %+v", g)
	}
	rr := tableRows(g.Blocks[0])
	// 11 reglas: las dos de 53317 sin comentario se juntan entre IPv4 e
	// IPv6; las de 1714:1764 no, porque la captura numera los comentarios
	// ("rule N") por archivo.
	if len(rr) != 9 || !reflect.DeepEqual(rr[5].Cells, []string{"Allow", "1714:1764", "TCP", "anywhere", "rule 3"}) ||
		!reflect.DeepEqual(rr[2].Cells, []string{"Allow", "53 → 172.17.0.1", "UDP", "172.16.0.0/12", "rule 1"}) ||
		!reflect.DeepEqual(rr[4].Cells, []string{"Allow", "2283", "TCP", "192.168.100.0/24", "—"}) {
		t.Errorf("rules = %+v", rr)
	}
	for k, want := range map[string]string{
		"/, /home, /var/cache/pacman/pkg, /var/log": "Encrypted (LUKS2 on nvme1n1p2)",
		"/boot":               "Not encrypted",
		"Swap /swap/swapfile": "Encrypted (LUKS2 on nvme1n1p2)",
		"Swap /dev/zram0":     "In memory",
	} {
		if got := kvValue(v.Blocks, "Disk encryption", k); got != want {
			t.Errorf("Disk encryption %s = %q, want %q", k, got, want)
		}
	}
	ex, gr, enc, pl := indexOf(v.Blocks, "table", "Exposed to the network"), indexOf(v.Blocks, "group", "Firewall rules"),
		indexOf(v.Blocks, "kv", "Disk encryption"), indexOf(v.Blocks, "kv", "Platform")
	if !(ex >= 0 && ex < gr && gr < enc && enc < pl) {
		t.Errorf("order: exposed %d, rules %d, encryption %d, platform %d", ex, gr, enc, pl)
	}
	h := hero(t, v)
	if f := statByLabel(h, "Firewall"); f.Text != "On" || f.Level != "ok" {
		t.Errorf("firewall stat = %+v", f)
	}
	// prog2 (open, tcp+udp): 2. avahi (limited, por la excepción de
	// mDNS de before.rules) no cuenta — Task 7 ruling: "Open to every
	// network" es solo open + none, no limited (un origen puntual como
	// mDNS no es "toda la red").
	if o := statByLabel(h, "Open to every network"); o.Text != "2" || o.Level != "" {
		t.Errorf("open stat = %+v", o)
	}
	noEmptyValues(t, v)
}

func TestSecurityViewSpanish(t *testing.T) {
	v, _ := Build("security", secSnap(t, "threadripper", "security"), Options{Lang: "es"})
	rs := tableRows(findTable(v.Blocks, "Expuesto a la red"))
	if len(rs) != 14 || rs[0].Cells[2] != "Todas las redes" || rs[0].Cells[4] != "Permitido desde cualquier lado" ||
		rs[2].Cells[3] != "proceso del sistema (avahi)" || rs[2].Cells[4] != "Permitido desde mDNS (multicast)" {
		t.Fatalf("rows = %+v", rs)
	}
	if kvValue(v.Blocks, "Cifrado de disco", "Swap /dev/zram0") != "En memoria" {
		t.Errorf("es encryption = %+v", v.Blocks)
	}
	if o := statByLabel(hero(t, v), "Abiertos a todas las redes"); o.Text != "2" {
		t.Errorf("es hero = %+v", hero(t, v))
	}
}

func TestSecurityFirewallLines(t *testing.T) {
	for _, c := range []struct {
		fw          model.Firewall
		text, level string
		noGreenOn   bool // review M5/I1: ACCEPT and RulesUnknown must not also show the everyday green line
	}{
		{model.Firewall{Tool: "ufw", State: "on", Active: true, Policy: "DROP", IPv6: true}, "Firewall on (ufw)", "ok", false},
		{model.Firewall{Tool: "ufw", State: "on", Policy: "ACCEPT", IPv6: true}, "Firewall on (ufw), but it lets everything in (incoming policy ACCEPT)", "warn", true},
		// Task 3 ruling: ufw activo con IPV6=no bloquea todo el IPv6 salvo
		// loopback, así que no hay nada que avisar: solo informa (la línea
		// verde de siempre sigue, aparte de esta).
		{model.Firewall{Tool: "ufw", State: "on", Active: true, Policy: "DROP"}, "ufw blocks all IPv6 except loopback (IPV6=no in /etc/default/ufw)", "", false},
		// Task 7 fix round 1 (review I1): ufw activado pero sin poder leer
		// sus reglas no es la línea verde de siempre — no se sabe qué deja
		// pasar.
		{model.Firewall{Tool: "ufw", State: "on", Active: true, Policy: "DROP", IPv6: true, RulesUnknown: true}, "ufw is on but its rules could not be read", "warn", true},
		{model.Firewall{Tool: "ufw", State: "not_loaded"}, "Firewall configured but not loaded (ufw.service is not running)", "warn", true},
		{model.Firewall{Tool: "ufw", State: "off"}, "Firewall off (ufw)", "", true},
		{model.Firewall{Tool: "firewalld", State: "running", Active: true}, "firewalld is running (rules not read)", "", true},
		{model.Firewall{Tool: "nftables", State: "running", Active: true}, "nftables is running (rules not read)", "", true},
		{model.Firewall{Tool: "iptables", State: "running", Active: true}, "iptables is running (rules not read)", "", true},
		{model.Firewall{State: "none"}, "No firewall", "", true},
	} {
		fw := c.fw
		s := &model.Snapshot{Lang: "en", Security: &model.Security{Listening: []model.Listener{}, Firewall: &fw}}
		v, _ := Build("security", s, Options{Lang: "en"})
		tx := texts(v)
		if lvl, ok := tx[c.text]; !ok || lvl != c.level {
			t.Errorf("%+v: texts = %v", c.fw, tx)
		}
		if lvl, ok := tx["Firewall on (ufw)"]; c.noGreenOn && ok && lvl == "ok" {
			t.Errorf("%+v: unexpected green 'Firewall on (ufw)': %v", c.fw, tx)
		}
	}
}

// secWith: active=false builds a genuinely off firewall (State "off"), not
// an impossible "on but not active without ACCEPT" state (review M5).
func secWith(active bool, ls []model.Listener, vols []model.CryptVolume, swaps []model.SwapCrypt) *model.Snapshot {
	state := "off"
	if active {
		state = "on"
	}
	return &model.Snapshot{Lang: "en", Security: &model.Security{Listening: ls,
		Firewall: &model.Firewall{Tool: "ufw", State: state, Active: active, IPv6: true}, Volumes: vols, Swaps: swaps}}
}

// secOff builds a snapshot with a firewall that doesn't filter (not Active).
func secOff(fw model.Firewall, ls []model.Listener) *model.Snapshot {
	return &model.Snapshot{Lang: "en", Security: &model.Security{Listening: ls, Firewall: &fw}}
}

// secAccept builds an ufw on, incoming policy ACCEPT firewall: it runs, but
// filters nothing (Active is false, same as ufw off, for a different reason).
func secAccept(ls []model.Listener) *model.Snapshot {
	return &model.Snapshot{Lang: "en", Security: &model.Security{Listening: ls,
		Firewall: &model.Firewall{Tool: "ufw", State: "on", Active: false, Policy: "ACCEPT", IPv6: true}}}
}

func warnLinks(bs []Block, section string) []string {
	var out []string
	for _, b := range bs {
		if b.Link == section && b.Level == "warn" {
			out = append(out, b.Text)
		}
	}
	return out
}

func TestSecurityWarnings(t *testing.T) {
	l := func(proto, addr string, port int, scope, fw string) model.Listener {
		return model.Listener{Proto: proto, Addr: addr, Port: port, Scope: scope, Firewall: fw, Owners: []model.ListenerOwner{}}
	}
	tcpOpen, tcpNone, tcpNone2 := l("tcp", "::", 1716, "all", "open"), l("tcp", "0.0.0.0", 22, "all", "none"), l("tcp", "::", 80, "all", "none")
	udpNone, local := l("udp", "0.0.0.0", 5353, "all", "none"), l("tcp", "127.0.0.1", 631, "local", "")
	// Task 3 ruling: con ufw activo, un puerto IPv6 en todas las redes queda
	// blocked (ufw-init con IPV6=no también bloquea; ver firewall.go), no
	// none: no hay nada que avisar.
	ipv6Blocked := l("tcp", "::", 443, "all", "blocked")
	root := []model.CryptVolume{{Device: "/dev/mapper/root", Mounts: []string{"/", "/home"}, Encrypted: true, Type: "LUKS2", On: "nvme1n1p2"}}
	for _, c := range []struct {
		name string
		s    *model.Snapshot
		want []string
	}{
		{"firewall on, port allowed", secWith(true, []model.Listener{tcpOpen, local}, nil, nil), nil},
		{"firewall off, one TCP", secWith(false, []model.Listener{tcpNone, udpNone, local}, nil, nil),
			[]string{"Firewall off and 1 service listens on every network"}},
		{"firewall off, only UDP", secWith(false, []model.Listener{udpNone, local}, nil, nil), nil},
		{"firewall off, two TCP", secWith(false, []model.Listener{tcpNone, tcpNone2}, nil, nil),
			[]string{"Firewall off and 2 services listen on every network"}},
		{"ufw on, IPv6 blocked", secWith(true, []model.Listener{ipv6Blocked}, nil, nil), nil},
		// Task 7 fix round 1 (review M2): ACCEPT is not "off" — the
		// firewall runs, it just lets everything in.
		{"ufw on, ACCEPT policy", secAccept([]model.Listener{tcpNone}),
			[]string{"Nothing filters incoming connections and 1 service listens on every network"}},
		// Final review M1: a dual-stack service (0.0.0.0 and :: on the
		// same port) is one service, not two.
		{"firewall off, dual-stack sshd", secWith(false, []model.Listener{tcpNone, l("tcp", "::", 22, "all", "none")}, nil, nil),
			[]string{"Firewall off and 1 service listens on every network"}},
		// Task 7 re-review N2: an ACCEPT policy only matters with ufw on.
		{"ufw off, ACCEPT policy", secOff(model.Firewall{Tool: "ufw", State: "off", Policy: "ACCEPT", IPv6: true}, []model.Listener{tcpNone}),
			[]string{"Firewall off and 1 service listens on every network"}},
		// Final review M8: enabled but ufw.service not running — it may
		// still be loaded some other way; don't claim it is off.
		{"ufw not loaded", secOff(model.Firewall{Tool: "ufw", State: "not_loaded", Policy: "DROP", IPv6: true}, []model.Listener{tcpNone}),
			[]string{"ufw.service is not running; the firewall may not be loaded"}},
		{"plain swap, encrypted root", secWith(true, nil, root, []model.SwapCrypt{{Name: "/dev/nvme0n1p1", Kind: "partition", State: "plain"},
			{Name: "/dev/zram0", Kind: "zram", State: "memory"}}), []string{"Swap is not encrypted (/dev/nvme0n1p1) while the system disk is"}},
		{"plain swap, plain root", secWith(true, nil, []model.CryptVolume{{Device: "/dev/sda2", Mounts: []string{"/"}}},
			[]model.SwapCrypt{{Name: "/dev/sda3", Kind: "partition", State: "plain"}}), nil},
		{"unknown swap", secWith(true, nil, root, []model.SwapCrypt{{Name: "/x/swap", Kind: "file", State: "unknown"}}), nil},
	} {
		if got := warnLinks(Warnings(c.s, Options{Lang: "en"}), "security"); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: warnings %q, want %q", c.name, got, c.want)
		}
		badge := -1
		for _, sec := range Sections(c.s, Options{Lang: "en"}) {
			if sec.ID == "security" {
				badge = sec.Badge
			}
		}
		if badge != len(c.want) {
			t.Errorf("%s: badge = %d, want %d", c.name, badge, len(c.want))
		}
	}
	v, _ := Build("security", secWith(false, []model.Listener{tcpNone, udpNone, local}, nil, nil), Options{Lang: "en"})
	h := hero(t, v)
	if f := statByLabel(h, "Firewall"); f.Text != "Off" || f.Level != "" {
		t.Errorf("firewall stat = %+v", f)
	}
	if o := statByLabel(h, "Open to every network"); o.Text != "2" || o.Level != "warn" {
		t.Errorf("open stat = %+v", o)
	}
	// ufw está presente (solo apagado), no ausente: "Not filtered", no el
	// más rotundo "No firewall" — Task 7 ruling.
	if rs := tableRows(findTable(v.Blocks, "Exposed to the network")); rs[0].Cells[4] != "Not filtered" {
		t.Errorf("rows = %+v", rs)
	}
	tx := texts(v)
	if lvl, ok := tx["Firewall off (ufw)"]; !ok || lvl != "" {
		t.Errorf("no genuine off line: %v", tx)
	}
	if lvl, ok := tx["Firewall on (ufw)"]; ok && lvl == "ok" {
		t.Errorf("green 'Firewall on' next to the off warning: %v", tx)
	}
}

// Final review M1: the hero counts services (protocol, port), not rows —
// dual-stack sshd on 0.0.0.0:22 and [::]:22 is one open service; TCP and
// UDP on the same port are two.
func TestSecurityHeroCountsServices(t *testing.T) {
	l := func(proto, addr string, port int) model.Listener {
		return model.Listener{Proto: proto, Addr: addr, Port: port, Scope: "all", Firewall: "none", Owners: []model.ListenerOwner{}}
	}
	v, _ := Build("security", secWith(false, []model.Listener{l("tcp", "0.0.0.0", 22), l("tcp", "::", 22), l("udp", "::", 22)}, nil, nil), Options{Lang: "en"})
	if o := statByLabel(hero(t, v), "Open to every network"); o.Text != "2" {
		t.Errorf("open stat = %+v, want 2", o)
	}
}

// Final review M4: a rule without a port shows "any" translated.
func TestSecurityRulesAnyPort(t *testing.T) {
	s := &model.Snapshot{Lang: "es", Security: &model.Security{Firewall: &model.Firewall{Tool: "ufw", State: "on", Active: true, IPv6: true,
		Rules: []model.FwRule{{Action: "allow", Proto: "any", Port: "any", To: "0.0.0.0/0", From: "192.168.1.0/24"}}}}}
	v, _ := Build("security", s, Options{Lang: "es"})
	g := findGroup(v.Blocks, "Reglas del firewall")
	if len(g.Blocks) != 1 {
		t.Fatalf("group = %+v", g)
	}
	if rr := tableRows(g.Blocks[0]); len(rr) != 1 || rr[0].Cells[1] != "cualquiera" || rr[0].Cells[2] != "cualquiera" {
		t.Errorf("rows = %+v", rr)
	}
}

func TestSecurityViewLaptop(t *testing.T) {
	s := secSnap(t, "laptop", "security")
	v, _ := Build("security", s, Options{Lang: "en"})
	if _, ok := texts(v)["No firewall"]; !ok || table(v, "Exposed to the network") != nil || Unavailable(s, "security.listening") == nil {
		t.Errorf("texts = %v", texts(v))
	}
	noEmptyValues(t, v)
}

// TestSecurityRulesUnknown covers review finding I1 (fix round 1): ufw on
// with its rules unreadable must not look like the everyday green "on", and
// with any all-scope listener unknown, the hero cannot claim a count.
func TestSecurityRulesUnknown(t *testing.T) {
	s := &model.Snapshot{Lang: "en", Security: &model.Security{
		Listening: []model.Listener{{Proto: "tcp", Addr: "0.0.0.0", Port: 22, Scope: "all", Firewall: "unknown", Owners: []model.ListenerOwner{}}},
		Firewall:  &model.Firewall{Tool: "ufw", State: "on", Active: true, Policy: "DROP", IPv6: true, RulesUnknown: true},
	}}
	v, _ := Build("security", s, Options{Lang: "en"})
	tx := texts(v)
	if lvl, ok := tx["ufw is on but its rules could not be read"]; !ok || lvl != "warn" {
		t.Errorf("texts = %v", tx)
	}
	if lvl, ok := tx["Firewall on (ufw)"]; ok && lvl == "ok" {
		t.Errorf("green 'Firewall on' with RulesUnknown: %v", tx)
	}
	if rs := tableRows(findTable(v.Blocks, "Exposed to the network")); len(rs) != 1 || rs[0].Cells[4] != "Rules not read" {
		t.Errorf("rows = %+v", rs)
	}
	h := hero(t, v)
	if f := statByLabel(h, "Firewall"); f.Text != "On" || f.Level != "" {
		t.Errorf("firewall stat = %+v (want On, not green)", f)
	}
	// Task 7 re-review N3: "?" is not a finding, just unknown — neutral level.
	if o := statByLabel(h, "Open to every network"); o.Text != "?" || o.Level != "" {
		t.Errorf("open stat = %+v, want \"?\" (neutral) instead of a falsely reassuring count", o)
	}
}

// TestSecurityUnknownFirewalld: firewalld/nftables' rules are never read
// either, so their all-scope listeners are also "unknown" and the hero's
// count is "?" — the firewall itself is genuinely active, though, so its own
// stat stays green.
func TestSecurityUnknownFirewalld(t *testing.T) {
	s := &model.Snapshot{Lang: "en", Security: &model.Security{
		Listening: []model.Listener{{Proto: "tcp", Addr: "0.0.0.0", Port: 80, Scope: "all", Firewall: "unknown", Owners: []model.ListenerOwner{}}},
		Firewall:  &model.Firewall{Tool: "firewalld", State: "running", Active: true},
	}}
	v, _ := Build("security", s, Options{Lang: "en"})
	h := hero(t, v)
	if o := statByLabel(h, "Open to every network"); o.Text != "?" || o.Level != "" {
		t.Errorf("open stat = %+v", o)
	}
	if f := statByLabel(h, "Firewall"); f.Text != "On" || f.Level != "ok" {
		t.Errorf("firewall stat = %+v", f)
	}
}

// TestOwnersText: uid < 1000 is a system account, uid >= 1000 (that is not
// the current user, who would be Own) is another human user on the machine
// — review M3, they were both labelled "system process".
func TestOwnersText(t *testing.T) {
	for _, c := range []struct {
		os   []model.ListenerOwner
		l    string
		want string
	}{
		{[]model.ListenerOwner{{Own: true, Program: "kdeconnectd"}}, "en", "kdeconnectd"},
		{[]model.ListenerOwner{{Own: true}}, "en", "a program of yours"},
		{[]model.ListenerOwner{{Own: true}}, "es", "un programa propio"},
		{[]model.ListenerOwner{{UID: 33, User: "avahi"}}, "en", "system process (avahi)"},
		{[]model.ListenerOwner{{UID: 999, User: "systemd-resolve"}}, "en", "system process (systemd-resolve)"},
		{[]model.ListenerOwner{{UID: 1000, User: "bob"}}, "en", "another user (bob)"},
		{[]model.ListenerOwner{{UID: 1001, User: "bob"}}, "en", "another user (bob)"},
		{[]model.ListenerOwner{{UID: 1001, User: "bob"}}, "es", "otro usuario (bob)"},
		{[]model.ListenerOwner{{UID: 1001}}, "en", "another user (1001)"},
		// Task 7 re-review N1: nobody (65534) and DynamicUser (61184–65519)
		// are system accounts, not people.
		{[]model.ListenerOwner{{UID: 60000, User: "carol"}}, "en", "another user (carol)"},
		{[]model.ListenerOwner{{UID: 60001}}, "en", "system process (60001)"},
		{[]model.ListenerOwner{{UID: 61184}}, "en", "system process (61184)"},
		{[]model.ListenerOwner{{UID: 65534, User: "nobody"}}, "en", "system process (nobody)"},
	} {
		if got := ownersText(c.os, c.l); got != c.want {
			t.Errorf("ownersText(%+v, %q) = %q, want %q", c.os, c.l, got, c.want)
		}
	}
}

// TestFromText covers the carry-in from Task 3: before.rules' fixed labels
// and the "anywhere → <dst>" / "<src> → <dst>" destination-specific shape,
// in English and Spanish (M5: untested before this fix round).
func TestFromText(t *testing.T) {
	for _, c := range []struct{ from, l, want string }{
		{"DHCP servers", "en", "DHCP servers"},
		{"DHCP servers", "es", "servidores DHCP"},
		{"mDNS (multicast)", "en", "mDNS (multicast)"},
		{"mDNS (multicast)", "es", "mDNS (multicast)"},
		{"SSDP (multicast)", "es", "SSDP (multicast)"},
		{"DHCPv6 servers", "es", "servidores DHCPv6"},
		{"anywhere → 172.17.0.1", "en", "anywhere → 172.17.0.1"},
		{"anywhere → 172.17.0.1", "es", "cualquier lado → 172.17.0.1"},
		{"172.16.0.0/12 → 172.17.0.1", "es", "172.16.0.0/12 → 172.17.0.1"},
		{"192.168.100.0/24", "es", "192.168.100.0/24"},
		{"192.168.100.0/24, 10.0.0.0/8", "en", "192.168.100.0/24, 10.0.0.0/8"},
	} {
		if got := fromText(c.from, c.l); got != c.want {
			t.Errorf("fromText(%q, %q) = %q, want %q", c.from, c.l, got, c.want)
		}
	}
}

// TestCleanComment covers the carry-in from Task 3's M9: control characters,
// Unicode format (Cf) characters and, since review M7, whole ANSI/CSI
// escape sequences — stripping only the ESC byte would leave the rest of
// the sequence ("31m") as visible garbage.
func TestCleanComment(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"KDE Connect", "KDE Connect"},
		{"allow-docker-dns", "allow-docker-dns"},
		{"a\x07b", "ab"},                  // bell (control), no CSI
		{"a‏b", "ab"},                     // RTL mark (Cf)
		{"\x1b[31mAlert\x1b[0m", "Alert"}, // one CSI sequence per side
		{"\x1b[1;32mgreen\x1b[0m and \x1b[31mred\x1b[0m", "green and red"},
		{"\x1bnot a CSI", "not a CSI"}, // lone ESC (no '['): only the ESC itself is control
	} {
		if got := cleanComment(c.in); got != c.want {
			t.Errorf("cleanComment(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
