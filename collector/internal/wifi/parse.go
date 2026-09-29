// Package wifi lee las redes Wi-Fi con nmcli y el radio con rfkill (/sys), y
// corre las acciones de Wi-Fi de la sección Red: conectar, desconectar,
// olvidar, prender y apagar el radio, buscar redes y fijar la banda. Ninguna
// contraseña pasa por acá: una red nueva con clave se conecta en nmtui.
package wifi

import (
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

// Bands son las opciones de la acción wifi.band (las de omarchy-network-band).
var Bands = []string{"auto", "2.4", "5", "6"}

// Fields separa una línea de `nmcli -t`: los campos van separados por ":", y
// ":" y "\" dentro de un valor vienen escapados como "\:" y "\\".
func Fields(line string) []string {
	var out []string
	var b strings.Builder
	esc := false
	for _, r := range line {
		switch {
		case esc:
			b.WriteRune(r)
			esc = false
		case r == '\\':
			esc = true
		case r == ':':
			out = append(out, b.String())
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	return append(out, b.String())
}

// BandOf es la banda de una frecuencia de nmcli ("5180 MHz"), con los límites
// de omarchy-network-band; "" si no cae en ninguna.
func BandOf(freq string) string {
	f := strings.Fields(freq)
	if len(f) == 0 {
		return ""
	}
	mhz, err := strconv.Atoi(f[0])
	if err != nil {
		return ""
	}
	switch {
	case mhz >= 2400 && mhz <= 2499:
		return "2.4"
	case mhz >= 4900 && mhz <= 5924:
		return "5"
	case mhz >= 5925 && mhz <= 7124:
		return "6"
	}
	return ""
}

// secOrder es el orden en que se muestran los tipos de seguridad conocidos.
var secOrder = []string{"WEP", "WPA1", "WPA2", "WPA3", "OWE"}

// Security normaliza la columna SECURITY: "" (abierta) si viene vacía o "--";
// 802.1X (empresarial) sola; si no, los tipos conocidos en orden (sin WPA1
// cuando también hay WPA2 o WPA3) y después los desconocidos.
func Security(raw string) string {
	seen := map[string]bool{}
	var other []string
	for _, t := range strings.Fields(raw) {
		if t == "--" || seen[t] {
			continue
		}
		seen[t] = true
		if t != "802.1X" && !slices.Contains(secOrder, t) {
			other = append(other, t)
		}
	}
	if seen["802.1X"] {
		return "802.1X"
	}
	if seen["WPA2"] || seen["WPA3"] {
		delete(seen, "WPA1")
	}
	var out []string
	for _, t := range secOrder {
		if seen[t] {
			out = append(out, t)
		}
	}
	return strings.Join(append(out, other...), " ")
}

// ParseList lee `nmcli -t -f IN-USE,SSID,SIGNAL,FREQ,SECURITY,BSSID device
// wifi list`: una red por SSID, la que está en uso o, si no, la de mejor
// señal. Las ocultas (sin SSID) van una por BSSID (en mayúsculas), con el
// mismo criterio; una oculta sin BSSID válido y las líneas rotas se saltean.
// Sale ordenada por señal, de mayor a menor.
func ParseList(out string) []model.WifiNet {
	byKey := map[string]int{}
	var nets []model.WifiNet
	for _, l := range strings.Split(out, "\n") {
		f := Fields(strings.TrimRight(l, "\r"))
		if len(f) < 6 {
			continue
		}
		sig, err := strconv.Atoi(strings.TrimSpace(f[2]))
		if err != nil || sig < 0 || sig > 100 {
			continue
		}
		n := model.WifiNet{SSID: f[1], Signal: sig, Band: BandOf(f[3]), Security: Security(f[4]), InUse: strings.TrimSpace(f[0]) == "*"}
		// La clave separa las ocultas de un SSID que se escriba igual que un
		// BSSID: "\x00" no puede ir en un SSID que nmcli muestre.
		key := n.SSID
		if n.SSID == "" {
			n.Hidden, n.BSSID = true, strings.ToUpper(strings.TrimSpace(f[5]))
			if !IsBSSID(n.BSSID) {
				continue
			}
			key = "\x00" + n.BSSID
		}
		i, ok := byKey[key]
		switch {
		case !ok:
			byKey[key] = len(nets)
			nets = append(nets, n)
		case n.InUse || !nets[i].InUse && n.Signal > nets[i].Signal:
			nets[i] = n
		}
	}
	sort.SliceStable(nets, func(a, b int) bool { return nets[a].Signal > nets[b].Signal })
	return nets
}

// Conn es una conexión guardada de NetworkManager.
type Conn struct{ Name, UUID, Type string }

// ParseConns lee `nmcli -t -f NAME,UUID,TYPE connection show`.
func ParseConns(out string) []Conn {
	var cs []Conn
	for _, l := range strings.Split(out, "\n") {
		f := Fields(strings.TrimRight(l, "\r"))
		if len(f) < 3 || f[1] == "" {
			continue
		}
		cs = append(cs, Conn{Name: f[0], UUID: f[1], Type: f[2]})
	}
	return cs
}

// Device es una placa según `nmcli -t -f DEVICE,STATE,CONNECTION device`.
type Device struct{ State, Conn string }

// ParseDevices lee el estado de cada placa, por nombre.
func ParseDevices(out string) map[string]Device {
	m := map[string]Device{}
	for _, l := range strings.Split(out, "\n") {
		f := Fields(strings.TrimRight(l, "\r"))
		if len(f) < 3 || f[0] == "" {
			continue
		}
		m[f[0]] = Device{State: f[1], Conn: f[2]}
	}
	return m
}

// Printable saca los caracteres de control de un texto que viene del aire (un
// SSID) antes de mostrarlo: en una terminal serían secuencias de escape.
func Printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// IsBSSID dice si s es un BSSID normalizado: seis pares hexadecimales en
// mayúsculas separados por ":".
func IsBSSID(s string) bool {
	if len(s) != 17 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case i%3 == 2:
			if c != ':' {
				return false
			}
		case c < '0' || c > '9' && c < 'A' || c > 'F':
			return false
		}
	}
	return true
}
