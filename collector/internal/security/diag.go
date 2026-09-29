package security

import (
	"encoding/binary"
	"errors"
	"net/netip"
	"syscall"

	"golang.org/x/sys/unix"
)

// Los puertos TCP que escuchan, en la máquina real, salen de netlink
// sock_diag y no de /proc/net/tcp{,6}: para armar esos archivos el kernel
// recorre toda la tabla hash de conexiones TCP establecidas (en una máquina
// con mucha RAM, 4 MiB: ~5 ms cada uno), aunque haya pocos sockets. Pidiendo
// solo el estado LISTEN, inet_diag recorre solo la tabla de los que
// escuchan (~0.2 ms). Es de solo lectura y no pide root; da el mismo uid e
// inodo que /proc.

const (
	tcpListen     = 10 // TCP_LISTEN
	nlHdrLen      = 16 // struct nlmsghdr
	diagReqLen    = 56 // struct inet_diag_req_v2
	diagMsgLen    = 72 // struct inet_diag_msg
	diagRecvBytes = 64 << 10
)

var errDiagTrunc = errors.New("sock_diag: truncated message")

// listenTCP lee los sockets TCP en LISTEN (IPv4 e IPv6) por netlink; una
// variable para que los tests simulen que falla.
var listenTCP = netlinkListeners

// diagRequest arma el pedido SOCK_DIAG_BY_FAMILY de un volcado de los
// sockets TCP de una familia en estado LISTEN (solo esa tabla).
func diagRequest(family uint8, seq uint32) []byte {
	ne := binary.NativeEndian
	b := make([]byte, nlHdrLen+diagReqLen)
	ne.PutUint32(b[0:], uint32(len(b)))
	ne.PutUint16(b[4:], unix.SOCK_DIAG_BY_FAMILY)
	ne.PutUint16(b[6:], unix.NLM_F_REQUEST|unix.NLM_F_DUMP)
	ne.PutUint32(b[8:], seq)
	// inet_diag_req_v2: familia, protocolo, extensiones (ninguna), relleno,
	// máscara de estados; el id del socket queda en cero (todos).
	b[16], b[17] = family, unix.IPPROTO_TCP
	ne.PutUint32(b[20:], 1<<tcpListen)
	return b
}

// parseDiag interpreta lo recibido de una lectura del socket netlink: uno o
// más mensajes. Devuelve los sockets en LISTEN de la secuencia seq, y done
// con NLMSG_DONE. Un NLMSG_ERROR, o un NLMSG_DONE con errno, es ese errno; un mensaje cortado o de una
// familia desconocida, un error (el que llama cae a /proc/net).
func parseDiag(b []byte, seq uint32) ([]Socket, bool, error) {
	ne := binary.NativeEndian
	var out []Socket
	for len(b) > 0 {
		if len(b) < nlHdrLen {
			return out, false, errDiagTrunc
		}
		n := int(ne.Uint32(b[0:]))
		if n < nlHdrLen || n > len(b) {
			return out, false, errDiagTrunc
		}
		typ, s, p := ne.Uint16(b[4:]), ne.Uint32(b[8:]), b[nlHdrLen:n]
		switch typ {
		case unix.NLMSG_DONE:
			// Lleva un int32: 0, o un errno negativo si el volcado se
			// cortó (sin él, en kernels viejos, es un fin normal).
			if len(p) >= 4 {
				if code := int32(ne.Uint32(p)); code != 0 {
					return out, false, syscall.Errno(-code)
				}
			}
			return out, true, nil
		case unix.NLMSG_ERROR:
			if len(p) < 4 {
				return out, false, errDiagTrunc
			}
			if code := int32(ne.Uint32(p)); code != 0 {
				return out, false, syscall.Errno(-code)
			}
			return out, true, nil
		case unix.SOCK_DIAG_BY_FAMILY:
			if s != seq {
				break
			}
			if len(p) < diagMsgLen {
				return out, false, errDiagTrunc
			}
			if p[1] != tcpListen {
				break
			}
			var a netip.Addr
			switch p[0] {
			case unix.AF_INET:
				a = netip.AddrFrom4([4]byte(p[8:12]))
			case unix.AF_INET6:
				a = netip.AddrFrom16([16]byte(p[8:24]))
			default:
				return out, false, errors.New("sock_diag: unknown family")
			}
			out = append(out, Socket{Proto: "tcp", Addr: a, Port: binary.BigEndian.Uint16(p[4:]),
				UID: ne.Uint32(p[64:]), Inode: uint64(ne.Uint32(p[68:]))})
		}
		n = (n + 3) &^ 3 // NLMSG_ALIGN
		if n > len(b) {
			n = len(b)
		}
		b = b[n:]
	}
	return out, false, nil
}

// netlinkListeners pide a sock_diag los sockets TCP en LISTEN de IPv4 y de
// IPv6, en un socket netlink que se cierra al terminar.
func netlinkListeners() ([]Socket, error) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, unix.NETLINK_SOCK_DIAG)
	if err != nil {
		return nil, err
	}
	defer unix.Close(fd)
	tv := unix.Timeval{Sec: 1}
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv); err != nil {
		return nil, err
	}
	buf := make([]byte, diagRecvBytes)
	out := []Socket{}
	for i, fam := range []uint8{unix.AF_INET, unix.AF_INET6} {
		seq := uint32(i + 1)
		if err := unix.Sendto(fd, diagRequest(fam, seq), 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
			return nil, err
		}
		for done := false; !done; {
			n, _, err := unix.Recvfrom(fd, buf, 0)
			if err == unix.EINTR {
				continue
			}
			if err != nil {
				return nil, err
			}
			if n == 0 {
				return nil, errDiagTrunc
			}
			var socks []Socket
			socks, done, err = parseDiag(buf[:n], seq)
			if err != nil {
				return nil, err
			}
			out = append(out, socks...)
		}
	}
	return out, nil
}
