package wifi

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// listFields son las columnas de `nmcli device wifi list`. Sin `-e no`: con
// -t, nmcli escapa ":" y "\" dentro de un valor (el SSID, el BSSID) y Fields
// los separa bien.
const listFields = "IN-USE,SSID,SIGNAL,FREQ,SECURITY,BSSID"

// Scan es una lectura del Wi-Fi.
type Scan struct {
	Manager string               // "nm", o "none" sin nmcli o sin NetworkManager
	Enabled bool                 // el Wi-Fi de NetworkManager (`nmcli radio wifi`)
	Ifaces  map[string]IfaceScan // por placa
	At      time.Time            // hora de la última lectura buena (cero: ninguna)
	Err     string               // esta lectura falló: el texto de nmcli; Ifaces es la anterior
}

// IfaceScan es lo leído de una placa.
type IfaceScan struct {
	Radio      string // on|soft|hard
	State      string // el de nmcli
	Connecting string // la red (su SSID si está guardada) a la que se está conectando
	Networks   []model.WifiNet
}

// Reader lee el Wi-Fi con nmcli. Guarda el SSID de cada conexión guardada
// (por UUID) y la última lectura buena. No es para uso concurrente: el engine
// lo usa desde una sola gorutina por vez.
type Reader struct {
	ssids map[string]string
	last  *Scan
}

// Read lee el radio, el estado de cada placa, las conexiones guardadas y, de
// cada placa encendida, las redes cercanas (--rescan no). at es la hora de la
// lectura (la pone quien llama: la gorutina del engine no toca el reloj).
func (r *Reader) Read(env source.Env, ifaces []string, at time.Time) *Scan {
	out, err := nm(env, "-t", "radio", "wifi")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || source.ExitStatus(err) == 8 { // 8: NetworkManager no corre
			return &Scan{Manager: "none", At: at, Ifaces: map[string]IfaceScan{}}
		}
		return r.failed(env, ifaces, out, err)
	}
	sc := &Scan{Manager: "nm", Enabled: strings.TrimSpace(string(out)) == "enabled", At: at, Ifaces: map[string]IfaceScan{}}
	out, err = nm(env, "-t", "-f", "DEVICE,STATE,CONNECTION", "device")
	if err != nil {
		return r.failed(env, ifaces, out, err)
	}
	devs := ParseDevices(string(out))
	out, err = nm(env, "-t", "-f", "NAME,UUID,TYPE", "connection", "show")
	if err != nil {
		return r.failed(env, ifaces, out, err)
	}
	saved, byName := r.saved(env, ParseConns(string(out)))
	for _, name := range ifaces {
		d := devs[name]
		is := IfaceScan{Radio: Radio(env.FS, name, sc.Enabled), State: d.State}
		if strings.HasPrefix(d.State, "connecting") {
			is.Connecting = cmp.Or(byName[d.Conn], d.Conn)
		}
		if is.Radio == "on" && d.State != "" && d.State != "unavailable" && d.State != "unmanaged" {
			out, err := nm(env, "-t", "-f", listFields, "device", "wifi", "list", "ifname", name, "--rescan", "no")
			if err != nil {
				return r.failed(env, ifaces, out, err)
			}
			is.Networks = ParseList(string(out))
			for i := range is.Networks {
				if u, ok := saved[is.Networks[i].SSID]; ok {
					is.Networks[i].Saved, is.Networks[i].UUID = true, u
				}
			}
		}
		sc.Ifaces[name] = is
	}
	r.last = sc
	return sc
}

// failed es la última lectura buena con la nota del error (o, si no hubo
// ninguna, solo la nota). Sin lista previa, el radio de cada placa sale de
// rfkill (no necesita nmcli): una placa apagada se ve apagada aunque nmcli
// no haya contestado.
func (r *Reader) failed(env source.Env, ifaces []string, out []byte, err error) *Scan {
	msg := Message(env.Lang, "nmcli", out, err)
	if r.last == nil {
		ifs := map[string]IfaceScan{}
		for _, name := range ifaces {
			ifs[name] = IfaceScan{Radio: Rfkill(env.FS, name)}
		}
		return &Scan{Manager: "nm", Ifaces: ifs, Err: msg}
	}
	sc := *r.last
	sc.Err = msg
	return &sc
}

// saved devuelve SSID → UUID de las conexiones Wi-Fi guardadas y nombre de
// la conexión → SSID. El SSID sale de `nmcli -g 802-11-wireless.ssid` (el
// nombre puede ser otro) y queda en caché por UUID.
func (r *Reader) saved(env source.Env, cs []Conn) (bySSID, byName map[string]string) {
	if r.ssids == nil {
		r.ssids = map[string]string{}
	}
	bySSID, byName = map[string]string{}, map[string]string{}
	for _, c := range cs {
		if c.Type != "802-11-wireless" {
			continue
		}
		ssid, ok := r.ssids[c.UUID]
		if !ok {
			out, err := nm(env, "-e", "no", "-g", "802-11-wireless.ssid", "connection", "show", c.UUID)
			if err != nil {
				continue
			}
			ssid = strings.TrimRight(string(out), "\n")
			r.ssids[c.UUID] = ssid
		}
		if ssid == "" {
			continue
		}
		if _, dup := bySSID[ssid]; !dup {
			bySSID[ssid] = c.UUID
		}
		byName[c.Name] = ssid
	}
	return bySSID, byName
}

func nm(env source.Env, args ...string) ([]byte, error) {
	return env.Run.Run(context.Background(), "nmcli", args...)
}

// Message es el error de un comando para el usuario: la primera línea de lo
// que dijo (stderr o, si no, la salida), sin "Error: ", recortada a 120
// caracteres.
func Message(lang, name string, out []byte, err error) string {
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return i18n.F(lang, "requires %s", name)
	case errors.Is(err, context.DeadlineExceeded):
		return i18n.T(lang, "timed out")
	}
	text := out
	var ee *exec.ExitError
	if errors.As(err, &ee) && len(bytes.TrimSpace(ee.Stderr)) > 0 {
		text = ee.Stderr
	}
	line := ""
	for _, l := range strings.Split(string(text), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			line = l
			break
		}
	}
	if line == "" {
		return err.Error()
	}
	line = strings.TrimPrefix(line, "Error: ")
	if rs := []rune(line); len(rs) > 120 {
		line = string(rs[:119]) + "…"
	}
	return line
}

// Apply copia la lectura a las placas Wi-Fi de n. Con sc nil les borra lo que
// agrega este paquete (la vista Red está cerrada o todavía no leyó) y deja lo
// de network (SSID, señal, tasa).
func Apply(n *model.Network, sc *Scan) {
	if n == nil {
		return
	}
	for i := range n.Interfaces {
		it := &n.Interfaces[i]
		if it.Kind != "wifi" {
			continue
		}
		if sc == nil {
			if w := it.Wifi; w != nil {
				w.Manager, w.Radio, w.State, w.Connecting, w.Error = "", "", "", "", ""
				w.Networks, w.Scanned = nil, time.Time{}
			}
			continue
		}
		if it.Wifi == nil {
			it.Wifi = &model.Wifi{}
		}
		is := sc.Ifaces[it.Name]
		w := it.Wifi
		w.Manager, w.Scanned, w.Error = sc.Manager, sc.At, sc.Err
		w.Radio, w.State, w.Connecting, w.Networks = is.Radio, is.State, is.Connecting, is.Networks
	}
}
