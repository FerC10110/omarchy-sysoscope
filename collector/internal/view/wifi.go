package view

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/wifi"
)

func init() {
	actions["network"] = wifiActions
}

// wifiBlocks: el estado del Wi-Fi de la placa y sus redes cercanas. Sin la
// lectura de nmcli todavía, "Buscando redes…".
func wifiBlocks(i model.Iface, l string) []Block {
	w := i.Wifi
	if w == nil || w.Manager == "" {
		return []Block{Text(i18n.T(l, "Looking for networks…"), "")}
	}
	if w.Manager == "none" {
		return []Block{Text(i18n.T(l, "Wi-Fi not available: NetworkManager is needed"), "warn")}
	}
	bs := []Block{Text(wifiStatus(w, l), "")}
	if w.Error != "" {
		if w.Scanned.IsZero() {
			bs = append(bs, Text(i18n.F(l, "Could not read the networks: %s", w.Error), "warn"))
		} else {
			bs = append(bs, Text(i18n.F(l, "Could not read the networks (%s); showing the list from %s", w.Error, w.Scanned.Format("15:04:05")), "warn"))
		}
	}
	// Una placa que NetworkManager no maneja se trata como unavailable: sin
	// lista de redes cercanas (regla del controller).
	if w.Radio != "on" || w.State == "unmanaged" {
		return bs
	}
	if len(w.Networks) == 0 {
		return append(bs, Text(i18n.T(l, "No networks in range"), ""))
	}
	cols := []string{i18n.T(l, "Network"), i18n.T(l, "Signal"), i18n.T(l, "Band"), i18n.T(l, "Security"), i18n.T(l, "State")}
	return append(bs, Table(i18n.T(l, "Nearby networks"), cols, wifiRows(i.Name, w, l), true))
}

// wifiStatus: apagado (por software o por hardware), sin manejar por
// NetworkManager, conectando, conectado (con banda y señal) o encendido sin
// conectar.
func wifiStatus(w *model.Wifi, l string) string {
	switch w.Radio {
	case "hard":
		return i18n.T(l, "Off (by hardware)")
	case "soft":
		return i18n.T(l, "Off (by software)")
	}
	if w.State == "unmanaged" {
		return i18n.T(l, "Not managed by NetworkManager")
	}
	if w.Connecting != "" {
		return i18n.F(l, "Connecting to \"%s\"…", wifi.Printable(w.Connecting))
	}
	if strings.HasPrefix(w.State, "connecting") {
		return i18n.T(l, "Connecting…")
	}
	in := inUse(w)
	if in == nil && w.State != "connected" {
		return i18n.T(l, "On, not connected")
	}
	ssid := w.SSID
	if ssid == "" && in != nil {
		ssid = in.SSID
	}
	parts := []string{i18n.F(l, "Connected to \"%s\"", wifi.Printable(ssid))}
	if in != nil && in.Band != "" {
		parts = append(parts, in.Band+" GHz")
	}
	if w.SignalDBm != nil {
		parts = append(parts, fmt.Sprintf("%d dBm", *w.SignalDBm))
	}
	return strings.Join(parts, " · ")
}

func inUse(w *model.Wifi) *model.WifiNet {
	for i := range w.Networks {
		if w.Networks[i].InUse {
			return &w.Networks[i]
		}
	}
	return nil
}

// wifiRows: la conectada, las guardadas, el resto y al final las ocultas,
// cada grupo por señal. El id es "<placa>:<ssid>" o, en una oculta,
// "<placa>:hidden:<BSSID>" (la acción corta en el primer ":" y wifi.resolve
// distingue las dos).
func wifiRows(iface string, w *model.Wifi, l string) []Row {
	nets := slices.Clone(w.Networks)
	rank := func(n model.WifiNet) int {
		switch {
		case n.Hidden:
			return 3
		case n.InUse:
			return 0
		case n.Saved:
			return 1
		}
		return 2
	}
	slices.SortStableFunc(nets, func(a, b model.WifiNet) int {
		return cmp.Or(rank(a)-rank(b), b.Signal-a.Signal, strings.Compare(a.SSID, b.SSID), strings.Compare(a.BSSID, b.BSSID))
	})
	rows := make([]Row, 0, len(nets))
	for _, n := range nets {
		band := ""
		if n.Band != "" {
			band = n.Band + " GHz"
		}
		sec := n.Security
		if sec == "" {
			sec = i18n.T(l, "open")
		}
		state := ""
		switch {
		case n.InUse:
			state = i18n.T(l, "connected")
		case w.Connecting != "" && w.Connecting == n.SSID:
			state = i18n.T(l, "connecting…")
		case n.Saved:
			state = i18n.T(l, "saved")
		}
		id, name := iface+":"+n.SSID, wifi.Printable(n.SSID)
		if n.Hidden {
			id, name = iface+":hidden:"+n.BSSID, i18n.T(l, "(hidden network)")
		}
		rows = append(rows, Row{ID: id, Cells: []string{name, fmt.Sprintf("%d%%", n.Signal), dash(band), wifi.Printable(sec), state},
			Flags: netFlags(n)})
	}
	return rows
}

// netFlags: connected o idle, saved o unsaved, y open (sin seguridad) o
// locked (con clave y no guardada: se conecta con nmtui). Una oculta lleva
// connected o idle y hidden, sin saved ni unsaved (no se sabe a qué
// conexión guardada corresponde). Las acciones se esconden con ellas (hide).
func netFlags(n model.WifiNet) []string {
	f := []string{"idle", "unsaved"}
	if n.InUse {
		f[0] = "connected"
	}
	if n.Hidden {
		return []string{f[0], "hidden"}
	}
	if n.Saved {
		f[1] = "saved"
	}
	switch {
	case n.Security == "":
		f = append(f, "open")
	case !n.Saved:
		f = append(f, "locked")
	}
	return f
}

// wifiActions son las acciones de Wi-Fi: solo con alguna placa que
// NetworkManager maneja. Apagar el radio y desconectar piden confirmación si
// la ruta por defecto sale por Wi-Fi.
func wifiActions(s *model.Snapshot, o Options) []Action {
	l := o.Lang
	out := []Action{}
	n := s.Network
	if n == nil {
		return out
	}
	var ws []*model.Wifi
	gwWifi := false
	for _, i := range n.Interfaces {
		if i.Kind == "wifi" && i.Wifi != nil && i.Wifi.Manager == "nm" {
			ws = append(ws, i.Wifi)
			gwWifi = gwWifi || i.Name == n.GatewayIface
		}
	}
	if len(ws) == 0 {
		return out
	}
	allHard, anyOn, anyConn, anyRow := true, false, false, false
	for _, w := range ws {
		allHard = allHard && w.Radio == "hard"
		anyOn = anyOn || w.Radio == "on"
		anyConn = anyConn || w.Radio == "on" && inUse(w) != nil
		anyRow = anyRow || w.Radio == "on" && len(w.Networks) > 0
	}
	if !allHard {
		a := Action{Key: "w", Label: i18n.T(l, "Wi-Fi on / off"), ID: "wifi.radio", Target: "section", Mode: "background"}
		if anyOn && gwWifi {
			a.Confirm = i18n.T(l, "Turn the Wi-Fi off? The default route goes through it.")
		}
		out = append(out, a)
	}
	if anyOn {
		out = append(out, Action{Key: "s", Label: i18n.T(l, "Search for networks"), ID: "wifi.rescan", Target: "section", Mode: "background"})
	}
	if anyConn {
		out = append(out, Action{Key: "b", Label: i18n.T(l, "Wi-Fi band"), ID: "wifi.band", Target: "section", Mode: "background",
			Input: &Input{Type: "choice", Prompt: i18n.T(l, "Band"), Options: wifi.Bands, Default: "auto"}})
	}
	if !anyRow {
		return out
	}
	disc := Action{Key: "d", Label: i18n.T(l, "Disconnect"), ID: "wifi.disconnect", Target: "row", Mode: "background", Hide: []string{"idle"}}
	if gwWifi {
		disc.Confirm = i18n.T(l, "Disconnect? The default route goes through this network.")
	}
	return append(out,
		Action{Key: "c", Label: i18n.T(l, "Connect"), ID: "wifi.connect", Target: "row", Mode: "background", Hide: []string{"connected", "locked", "hidden"}},
		Action{Key: "c", Label: i18n.T(l, "Connect with a password (nmtui)"), ID: "wifi.connect_new", Target: "row", Mode: "background",
			Hide: []string{"connected", "saved", "open", "hidden"}},
		// Solo en una oculta sin conectar: las con nombre llevan saved o
		// unsaved y las ocultas no.
		Action{Key: "c", Label: i18n.T(l, "Connect to a hidden network"), ID: "wifi.connect_hidden", Target: "row", Mode: "background",
			Hide: []string{"connected", "saved", "unsaved"}},
		disc,
		Action{Key: "x", Label: i18n.T(l, "Forget"), ID: "wifi.forget", Target: "row", Mode: "background", Hide: []string{"unsaved", "hidden"},
			Confirm: i18n.T(l, "Forget this network? Its saved password is deleted too.")},
	)
}
