package security

import (
	"encoding/binary"
	"errors"
	"net/netip"
	"os"
	"reflect"
	"slices"
	"syscall"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
	"golang.org/x/sys/unix"
)

var ne = binary.NativeEndian

// nlHdr arma una cabecera de netlink de un mensaje de n bytes de datos.
func nlHdr(typ uint16, seq uint32, n int) []byte {
	b := make([]byte, 16)
	ne.PutUint32(b[0:], uint32(16+n))
	ne.PutUint16(b[4:], typ)
	ne.PutUint16(b[6:], unix.NLM_F_MULTI)
	ne.PutUint32(b[8:], seq)
	return b
}

// diagMsg arma un inet_diag_msg de SOCK_DIAG_BY_FAMILY, como lo manda el
// kernel: puerto en big-endian, dirección en el orden de la red.
func diagMsg(seq uint32, fam uint8, state uint8, addr string, port uint16, uid, inode uint32) []byte {
	p := make([]byte, 72)
	p[0], p[1] = fam, state
	binary.BigEndian.PutUint16(p[4:], port)
	a := netip.MustParseAddr(addr).AsSlice()
	copy(p[8:24], a)
	ne.PutUint32(p[64:], uid)
	ne.PutUint32(p[68:], inode)
	return append(nlHdr(unix.SOCK_DIAG_BY_FAMILY, seq, len(p)), p...)
}

func nlDone(seq uint32) []byte {
	return append(nlHdr(unix.NLMSG_DONE, seq, 4), 0, 0, 0, 0)
}

func nlErr(seq uint32, errno int32) []byte {
	p := make([]byte, 20)
	ne.PutUint32(p, uint32(errno))
	return append(nlHdr(unix.NLMSG_ERROR, seq, len(p)), p...)
}

func TestDiagRequest(t *testing.T) {
	for _, fam := range []uint8{unix.AF_INET, unix.AF_INET6} {
		b := diagRequest(fam, 7)
		if len(b) != 72 || ne.Uint32(b[0:]) != 72 || ne.Uint16(b[4:]) != unix.SOCK_DIAG_BY_FAMILY ||
			ne.Uint16(b[6:]) != unix.NLM_F_REQUEST|unix.NLM_F_DUMP || ne.Uint32(b[8:]) != 7 {
			t.Fatalf("header % x", b[:16])
		}
		// inet_diag_req_v2: familia, protocolo, ext, pad, estados = solo LISTEN.
		if b[16] != fam || b[17] != unix.IPPROTO_TCP || b[18] != 0 || ne.Uint32(b[20:]) != 1<<10 {
			t.Errorf("request % x", b[16:24])
		}
		for _, x := range b[24:] {
			if x != 0 {
				t.Fatalf("socket id must be zero: % x", b[24:])
			}
		}
	}
}

func TestParseDiag(t *testing.T) {
	buf := slices.Concat(
		diagMsg(3, unix.AF_INET, 10, "127.0.0.1", 631, 0, 22127),
		diagMsg(3, unix.AF_INET, 10, "172.17.0.1", 53, 974, 21853),
	)
	got, done, err := parseDiag(buf, 3)
	want := []Socket{
		{Proto: "tcp", Addr: netip.MustParseAddr("127.0.0.1"), Port: 631, UID: 0, Inode: 22127},
		{Proto: "tcp", Addr: netip.MustParseAddr("172.17.0.1"), Port: 53, UID: 974, Inode: 21853},
	}
	if err != nil || done || !reflect.DeepEqual(got, want) {
		t.Fatalf("v4: %+v done=%v err=%v", got, done, err)
	}

	// IPv6, una v4-mapped (queda como la da /proc/net/tcp6: sin Unmap), otra
	// secuencia (se ignora), un estado que no es LISTEN y el fin.
	buf = slices.Concat(
		diagMsg(4, unix.AF_INET6, 10, "::", 1716, 1000, 27057),
		diagMsg(4, unix.AF_INET6, 10, "::ffff:192.168.1.5", 8080, 1000, 5),
		diagMsg(9, unix.AF_INET6, 10, "::1", 1, 1, 1),
		diagMsg(4, unix.AF_INET6, 1, "::1", 2, 2, 2),
		nlDone(4),
		diagMsg(4, unix.AF_INET6, 10, "::1", 3, 3, 3),
	)
	got, done, err = parseDiag(buf, 4)
	want = []Socket{
		{Proto: "tcp", Addr: netip.MustParseAddr("::"), Port: 1716, UID: 1000, Inode: 27057},
		{Proto: "tcp", Addr: netip.MustParseAddr("::ffff:192.168.1.5"), Port: 8080, UID: 1000, Inode: 5},
	}
	if err != nil || !done || !reflect.DeepEqual(got, want) {
		t.Fatalf("v6: %+v done=%v err=%v", got, done, err)
	}
	if !got[1].Addr.Is4In6() {
		t.Error("v4-mapped must stay mapped, like ParseNet")
	}
	// Lo mismo que ParseNet de la línea equivalente de /proc/net/tcp6.
	line := "   0: 0000000000000000FFFF00000501A8C0:1F90 00000000000000000000000000000000:0000 0A 0 0 0 1000 0 5\n"
	if p := ParseNet(line, "tcp", 32768, 60999); !reflect.DeepEqual(p, want[1:]) {
		t.Errorf("ParseNet %+v, netlink %+v", p, want[1:])
	}

	// Solo el fin: nada, terminado.
	if got, done, err := parseDiag(nlDone(4), 4); err != nil || !done || len(got) != 0 {
		t.Errorf("done only: %+v %v %v", got, done, err)
	}
	// NLMSG_DONE lleva un int32: un errno negativo (el volcado se cortó) es
	// un error, no un fin normal (el que llama cae a /proc/net).
	doneErr := append(nlHdr(unix.NLMSG_DONE, 4, 4), 0, 0, 0, 0)
	code := -int32(syscall.EINTR)
	ne.PutUint32(doneErr[16:], uint32(code))
	if _, _, err := parseDiag(slices.Concat(diagMsg(4, unix.AF_INET, 10, "127.0.0.1", 1, 1, 1), doneErr), 4); !errors.Is(err, syscall.EINTR) {
		t.Errorf("done with errno: err = %v, want EINTR", err)
	}
	// Un NLMSG_DONE sin el int32 (kernels viejos) sigue siendo el fin.
	if _, done, err := parseDiag(nlHdr(unix.NLMSG_DONE, 4, 0), 4); err != nil || !done {
		t.Errorf("bare done: %v %v", done, err)
	}
}

func TestParseDiagErrors(t *testing.T) {
	// NLMSG_ERROR con -EPERM.
	_, _, err := parseDiag(nlErr(1, -int32(syscall.EPERM)), 1)
	if !errors.Is(err, syscall.EPERM) {
		t.Errorf("err = %v, want EPERM", err)
	}
	// Truncados: cabecera corta, largo mayor que lo recibido, mensaje de
	// datos más corto que inet_diag_msg, largo menor que la cabecera.
	good := diagMsg(1, unix.AF_INET, 10, "127.0.0.1", 1, 1, 1)
	short := diagMsg(1, unix.AF_INET, 10, "127.0.0.1", 1, 1, 1)[:40]
	ne.PutUint32(short, 40)
	under := slices.Clone(good)
	ne.PutUint32(under, 8)
	for name, b := range map[string][]byte{
		"header":  good[:10],
		"length":  good[:60],
		"payload": short,
		"under":   under,
		"errmsg":  nlHdr(unix.NLMSG_ERROR, 1, 0),
		"family":  diagMsg(1, 99, 10, "127.0.0.1", 1, 1, 1),
	} {
		if _, _, err := parseDiag(b, 1); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// Con Root (fixtures) no se usa netlink; sin Root, si netlink falla, TCP
// sale de /proc/net/tcp*.
func TestReadSocketsNetlinkChoice(t *testing.T) {
	called := 0
	orig := listenTCP
	t.Cleanup(func() { listenTCP = orig })
	listenTCP = func() ([]Socket, error) { called++; return nil, syscall.EPROTONOSUPPORT }

	env := source.Fixture(t, "threadripper")
	if socks := ReadSockets(env, &collect.Report{Lang: "en"}); len(socks) != 16 || called != 0 {
		t.Fatalf("fixture: %d sockets, netlink called %d times", len(socks), called)
	}
	if _, err := os.Stat("/proc/net/tcp"); err != nil {
		t.Skip("no /proc/net/tcp")
	}
	// Los sockets pueden cambiar entre la lectura de ReadSockets y la de
	// acá: se reintenta, como en TestNetlinkMatchesProc.
	live := source.Env{}
	r := &collect.Report{Lang: "en"}
	var socks []Socket
	tcp, want := -1, 0
	for try := range 5 {
		r = &collect.Report{Lang: "en"}
		socks = ReadSockets(live, r)
		if called != try+1 || socks == nil || len(r.List) != 0 {
			t.Fatalf("fallback: called %d, %d sockets, %+v", called, len(socks), r.List)
		}
		want = 0
		for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
			data, _ := os.ReadFile(f)
			want += len(ParseNet(string(data), "tcp", 0, 0))
		}
		tcp = 0
		for _, s := range socks {
			if s.Proto == "tcp" {
				tcp++
			}
		}
		if tcp == want {
			break
		}
	}
	if tcp != want {
		t.Errorf("fallback tcp = %d, /proc = %d", tcp, want)
	}
	called = 1

	listenTCP = func() ([]Socket, error) {
		called++
		return []Socket{{Proto: "tcp", Addr: netip.MustParseAddr("::"), Port: 9, UID: 1, Inode: 1}}, nil
	}
	socks = ReadSockets(live, r)
	tcp = 0
	for _, s := range socks {
		if s.Proto == "tcp" {
			tcp++
		}
	}
	if called != 2 || tcp != 1 {
		t.Errorf("netlink: called %d, tcp %d (the /proc tcp tables must not be read)", called, tcp)
	}
}

// En la máquina real (solo lectura): netlink da los mismos sockets en
// LISTEN que /proc/net/tcp y tcp6. Los sockets pueden cambiar entre las dos
// lecturas: se reintenta.
func TestNetlinkMatchesProc(t *testing.T) {
	if _, err := netlinkListeners(); err != nil {
		t.Skipf("netlink sock_diag unavailable: %v", err)
	}
	var nl, proc []Socket
	for range 5 {
		var err error
		nl, err = netlinkListeners()
		if err != nil {
			t.Fatal(err)
		}
		proc = nil
		for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Skip(err)
			}
			proc = append(proc, ParseNet(string(data), "tcp", 0, 0)...)
		}
		sortSocks(nl)
		sortSocks(proc)
		if reflect.DeepEqual(nl, proc) {
			return
		}
	}
	t.Errorf("netlink %+v\n/proc %+v", nl, proc)
}

func sortSocks(s []Socket) {
	slices.SortFunc(s, func(a, b Socket) int {
		if c := a.Addr.Compare(b.Addr); c != 0 {
			return c
		}
		if a.Port != b.Port {
			return int(a.Port) - int(b.Port)
		}
		return int(a.Inode) - int(b.Inode)
	})
}
