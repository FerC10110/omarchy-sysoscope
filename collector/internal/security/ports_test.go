package security

import (
	"net/netip"
	"reflect"
	"slices"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

func TestReadSocketsThreadripper(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	r := &collect.Report{Lang: "en"}
	socks := ReadSockets(env, r)
	if len(r.List) != 0 || len(socks) != 16 {
		t.Fatalf("%d sockets, unavailable %+v", len(socks), r.List)
	}
	for _, want := range []Socket{
		{Proto: "tcp", Addr: netip.MustParseAddr("::"), Port: 1716, UID: 1000, Inode: 27057},
		{Proto: "tcp", Addr: netip.MustParseAddr("172.17.0.1"), Port: 53, UID: 974, Inode: 21853},
		{Proto: "tcp", Addr: netip.MustParseAddr("::1"), Port: 631, UID: 0, Inode: 22127},
		{Proto: "udp", Addr: netip.MustParseAddr("fe80::1"), Port: 546, UID: 0, Inode: 30274},
		{Proto: "udp", Addr: netip.MustParseAddr("224.0.0.251"), Port: 5353, UID: 1000, Inode: 42401},
		{Proto: "udp", Addr: netip.MustParseAddr("0.0.0.0"), Port: 5353, UID: 967, Inode: 9481},
	} {
		if !slices.Contains(socks, want) {
			t.Errorf("missing %+v", want)
		}
	}
	for _, s := range socks {
		if s.Inode == 61234 || s.Inode == 60001 || s.Inode == 60002 {
			t.Errorf("not a listening socket: %+v (established, ephemeral or connected)", s)
		}
	}
}

func TestParseNetGarbage(t *testing.T) {
	data := "  sl  local_address rem_address   st\n" +
		"garbage\n" +
		"   0: ZZZZ:0035 00000000:0000 0A a b c 5 0 1\n" +
		"   1: 0100007F 00000000:0000 0A a b c 5 0 2\n" +
		"   2: 01007F:0035 00000000:0000 0A a b c 5 0 3\n" +
		"   3: 0100007F:XYZ 00000000:0000 0A a b c 5 0 4\n" +
		"   4: 0100007F:0035 00000000:0000 0A a b c notauid 0 5\n" +
		"   5: 0100007F:0035\n" +
		"   6: 0100007F:0035 00000000:0000 0A a b c 5 0 notaninode\n" +
		"   7: 0100007F:0035 00000000:0000 0A a b c 5 0 11\n"
	got := ParseNet(data, "tcp", 32768, 60999)
	want := []Socket{{Proto: "tcp", Addr: netip.MustParseAddr("127.0.0.1"), Port: 53, UID: 5, Inode: 11}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	if got := ParseNet("", "udp", 1, 2); got != nil {
		t.Errorf("empty = %+v", got)
	}
}

func TestReadSocketsUnreadableAndDefaults(t *testing.T) {
	r := &collect.Report{Lang: "en"}
	env := source.Env{FS: fsWith(t, map[string]string{"/etc/hostname": "x\n"}), Lang: "en"}
	if s := ReadSockets(env, r); s != nil || len(r.List) != 1 || r.List[0].What != "security.listening" || r.List[0].Reason != "not_present" {
		t.Fatalf("sockets %+v, unavailable %+v", s, r.List)
	}
	// Sin ip_local_port_range, el rango por defecto: 41394 es efímero.
	r = &collect.Report{Lang: "en"}
	env.FS = fsWith(t, map[string]string{"/proc/net/udp": "sl\n" +
		" 1: 00000000:A1B2 00000000:0000 07 a b c 1000 0 1\n" +
		" 2: 00000000:0035 00000000:0000 07 a b c 0 0 2\n",
		"/proc/net/tcp": "sl\n"})
	socks := ReadSockets(env, r)
	if len(r.List) != 0 || len(socks) != 1 || socks[0].Inode != 2 {
		t.Fatalf("sockets %+v, unavailable %+v", socks, r.List)
	}
	env.FS = fsWith(t, map[string]string{"/proc/net/tcp": "sl\n"})
	if s := ReadSockets(env, r); s == nil || len(s) != 0 {
		t.Errorf("a readable empty table = %#v, want []", s)
	}
}

func TestUsers(t *testing.T) {
	env := source.Env{FS: fsWith(t, map[string]string{"/etc/passwd": "root:x:0:0::/root:/bin/bash\n# comment\nbroken\n" +
		"avahi:x:967:967:Avahi:/:/usr/bin/nologin\nuser:x:1000:\ndup:x:0:\n:x:5:\nbad:x:notanumber:\n"})}
	want := map[uint32]string{0: "root", 967: "avahi", 1000: "user"}
	if got := Users(env); !reflect.DeepEqual(got, want) {
		t.Fatalf("users = %v", got)
	}
	if got := Users(source.Env{FS: fsWith(t, nil)}); len(got) != 0 {
		t.Errorf("without /etc/passwd = %v", got)
	}
}

func TestListeningScopesAndOrder(t *testing.T) {
	a := netip.MustParseAddr
	socks := []Socket{
		{"udp", a("224.0.0.251"), 5353, 1000, 1},
		{"tcp", a("127.0.0.53"), 53, 974, 2},
		{"tcp", a("::"), 1716, 1000, 3},
		{"udp", a("fe80::1"), 546, 0, 4},
		{"udp", a("172.17.0.1"), 53, 974, 5},
		{"udp", a("0.0.0.0"), 5353, 967, 6},
		{"tcp", a("::ffff:127.0.0.1"), 8080, 1000, 7},
		{"udp", a("ff02::fb"), 5353, 1000, 8},
		{"tcp", a("192.0.2.9"), 22, 0, 9},
	}
	ifaces := []model.Iface{
		{Name: "docker0", IPv4: []string{"172.17.0.1/16"}},
		{Name: "enp5s0", IPv4: []string{"192.168.1.5/24"}, IPv6: []string{"fe80::1/64"}},
	}
	type row struct {
		proto, addr  string
		port         int
		scope, iface string
	}
	var got []row
	for _, l := range Listening(socks, ifaces, nil, 1000, nil) {
		got = append(got, row{l.Proto, l.Addr, l.Port, l.Scope, l.Iface})
	}
	want := []row{
		{"tcp", "::", 1716, "all", ""},
		{"udp", "0.0.0.0", 5353, "all", ""},
		{"tcp", "192.0.2.9", 22, "addr", ""},
		{"udp", "172.17.0.1", 53, "addr", "docker0"},
		{"udp", "fe80::1", 546, "link", "enp5s0"},
		{"udp", "224.0.0.251", 5353, "multicast", ""},
		{"udp", "ff02::fb", 5353, "multicast", ""},
		{"tcp", "127.0.0.53", 53, "local", ""},
		{"tcp", "127.0.0.1", 8080, "local", ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	if Listening(nil, nil, nil, 0, nil) != nil {
		t.Error("unread sockets must stay nil")
	}
	if l := Listening([]Socket{}, nil, nil, 0, nil); l == nil || len(l) != 0 {
		t.Errorf("no sockets = %#v, want []", l)
	}
}

func TestListeningOwners(t *testing.T) {
	a := netip.MustParseAddr
	socks := []Socket{
		{"tcp", a("::"), 1716, 1000, 27057},
		{"udp", a("224.0.0.251"), 5353, 1000, 1},
		{"udp", a("224.0.0.251"), 5353, 1000, 2},
		{"udp", a("224.0.0.251"), 5353, 1000, 3}, // su proceso no apareció
		{"udp", a("0.0.0.0"), 5353, 967, 4},
		{"udp", a("0.0.0.0"), 5353, 4242, 5}, // uid sin nombre
		{"tcp", a("0.0.0.0"), 1716, 1000, 6}, // el mismo servicio en IPv4: otra fila
	}
	users := map[uint32]string{967: "avahi", 1000: "user"}
	names := map[uint64]string{27057: "kdeconnectd", 1: "chromium", 2: "chromium", 6: "kdeconnectd"}
	prog := func(i uint64) (string, bool) { n, ok := names[i]; return n, ok }
	ls := Listening(socks, nil, users, 1000, prog)
	own := func(p string) []model.ListenerOwner {
		return []model.ListenerOwner{{UID: 1000, User: "user", Program: p, Own: true}}
	}
	want := [][]model.ListenerOwner{
		own("kdeconnectd"),                       // tcp 0.0.0.0:1716
		own("kdeconnectd"),                       // tcp [::]:1716
		{{UID: 967, User: "avahi"}, {UID: 4242}}, // udp 0.0.0.0:5353
		own("chromium"),                          // udp 224.0.0.251:5353
	}
	if len(ls) != 4 || ls[0].Addr != "0.0.0.0" || ls[1].Addr != "::" {
		t.Fatalf("listeners = %+v", ls)
	}
	for i, w := range want {
		if !reflect.DeepEqual(ls[i].Owners, w) {
			t.Errorf("%s %s:%d owners = %+v, want %+v", ls[i].Proto, ls[i].Addr, ls[i].Port, ls[i].Owners, w)
		}
	}
	// Sin búsqueda de programas (Seguridad cerrada): los propios, sin programa.
	if ls := Listening(socks, nil, users, 1000, nil); !reflect.DeepEqual(ls[3].Owners, own("")) {
		t.Errorf("without programs = %+v", ls[3].Owners)
	}
}
