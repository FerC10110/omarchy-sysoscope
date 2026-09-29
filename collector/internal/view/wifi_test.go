package view

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

// wifiSnap: la ruta por defecto va por cable; wlp3s0 conectada a "Casa",
// wlp4s0 apagada por hardware.
func wifiSnap() *model.Snapshot {
	dbm := -52
	return &model.Snapshot{Schema: 1, Lang: "en", Network: &model.Network{GatewayIface: "enp5s0", Interfaces: []model.Iface{
		{Name: "enp5s0", Kind: "ethernet", Up: true},
		{Name: "wlp3s0", Kind: "wifi", Up: true, Wifi: &model.Wifi{SSID: "Casa", SignalDBm: &dbm, Manager: "nm", Radio: "on", State: "connected",
			Scanned: time.Date(2026, 9, 26, 18, 30, 0, 0, time.UTC),
			Networks: []model.WifiNet{
				{SSID: "Vecino 🚀", Signal: 48, Band: "6", Security: "WPA3"},
				{SSID: "Café: 2º piso", Signal: 55, Band: "2.4", Security: "WPA2", Saved: true, UUID: "u3"},
				{SSID: "Plaza libre", Signal: 30, Band: "2.4"},
				{SSID: "Casa", Signal: 78, Band: "5", Security: "WPA2", Saved: true, UUID: "u2", InUse: true},
				{SSID: "Lejana", Signal: 70, Band: "5", Security: "WPA2"},
			}}},
		{Name: "wlp4s0", Kind: "wifi", Wifi: &model.Wifi{Manager: "nm", Radio: "hard", State: "unavailable"}},
	}}}
}

func iface(s *model.Snapshot, name string) *model.Iface {
	for i := range s.Network.Interfaces {
		if s.Network.Interfaces[i].Name == name {
			return &s.Network.Interfaces[i]
		}
	}
	return nil
}

func buildWifi(t *testing.T, s *model.Snapshot, lang string) *View {
	t.Helper()
	v, err := Build("network", s, Options{Lang: lang})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// rowActions son los ids de las acciones de fila que el panel ofrece en r (hide).
func rowActions(acts []Action, r Row) []string {
	var ids []string
	for _, a := range acts {
		if a.Target != "row" || slices.ContainsFunc(a.Hide, func(f string) bool { return slices.Contains(r.Flags, f) }) {
			continue
		}
		ids = append(ids, a.ID)
	}
	return ids
}

func actionByID(acts []Action, id string) *Action {
	for i := range acts {
		if acts[i].ID == id {
			return &acts[i]
		}
	}
	return nil
}

func TestWifiTable(t *testing.T) {
	v := buildWifi(t, wifiSnap(), "en")
	g := findGroup(v.Blocks, "wlp3s0")
	if len(g.Blocks) != 3 || g.Blocks[1].Text != `Connected to "Casa" · 5 GHz · -52 dBm` {
		t.Fatalf("wlp3s0 group = %+v", g.Blocks)
	}
	tb := g.Blocks[2]
	rows := tableRows(tb)
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	want := []string{"wlp3s0:Casa", "wlp3s0:Café: 2º piso", "wlp3s0:Lejana", "wlp3s0:Vecino 🚀", "wlp3s0:Plaza libre"}
	if tb.Type != "table" || !tb.Selectable || tb.Title != "Nearby networks" || !slices.Equal(ids, want) {
		t.Fatalf("table = %+v, ids %q", tb, ids)
	}
	if !slices.Equal(tb.Columns, []string{"Network", "Signal", "Band", "Security", "State"}) ||
		!slices.Equal(rows[0].Cells, []string{"Casa", "78%", "5 GHz", "WPA2", "connected"}) ||
		rows[1].Cells[4] != "saved" || rows[2].Cells[4] != "" || rows[4].Cells[3] != "open" {
		t.Fatalf("rows = %+v", rows)
	}
	// wlp4s0 apagada: su estado y nada más.
	g4 := findGroup(v.Blocks, "wlp4s0")
	if len(g4.Blocks) != 2 || g4.Blocks[1].Text != "Off (by hardware)" {
		t.Fatalf("wlp4s0 group = %+v", g4.Blocks)
	}
}

// Cada fila ofrece solo lo que le corresponde: una sola "c" y nunca en la conectada.
func TestWifiRowActions(t *testing.T) {
	v := buildWifi(t, wifiSnap(), "en")
	rows := tableRows(findGroup(v.Blocks, "wlp3s0").Blocks[2])
	want := map[string][]string{
		"wlp3s0:Casa":          {"wifi.disconnect", "wifi.forget"},
		"wlp3s0:Café: 2º piso": {"wifi.connect", "wifi.forget"},
		"wlp3s0:Lejana":        {"wifi.connect_new"},
		"wlp3s0:Vecino 🚀":      {"wifi.connect_new"},
		"wlp3s0:Plaza libre":   {"wifi.connect"},
	}
	for _, r := range rows {
		if got := rowActions(v.Actions, r); !slices.Equal(got, want[r.ID]) {
			t.Errorf("%s: %q, want %q (flags %q)", r.ID, got, want[r.ID], r.Flags)
		}
	}
	var section []string
	for _, a := range v.Actions {
		if a.Target == "section" {
			section = append(section, a.Key+" "+a.ID)
		}
		if a.Key == "enter" {
			t.Error("enter is lazylazy's own")
		}
	}
	if !slices.Equal(section, []string{"w wifi.radio", "s wifi.rescan", "b wifi.band"}) {
		t.Fatalf("section actions = %q", section)
	}
	b := actionByID(v.Actions, "wifi.band")
	if b.Input == nil || b.Input.Type != "choice" || !slices.Equal(b.Input.Options, []string{"auto", "2.4", "5", "6"}) || b.Input.Default != "auto" {
		t.Fatalf("band input = %+v", b.Input)
	}
}

// Apagar o desconectar solo pregunta si la ruta por defecto sale por Wi-Fi;
// olvidar pregunta siempre.
func TestWifiConfirmations(t *testing.T) {
	s := wifiSnap()
	v := buildWifi(t, s, "en")
	if actionByID(v.Actions, "wifi.radio").Confirm != "" || actionByID(v.Actions, "wifi.disconnect").Confirm != "" ||
		actionByID(v.Actions, "wifi.forget").Confirm == "" {
		t.Fatalf("with the default route on the cable: %+v", v.Actions)
	}
	s.Network.GatewayIface = "wlp3s0"
	v = buildWifi(t, s, "en")
	if actionByID(v.Actions, "wifi.radio").Confirm == "" || actionByID(v.Actions, "wifi.disconnect").Confirm == "" {
		t.Fatalf("with the default route on Wi-Fi: %+v", v.Actions)
	}
}

func TestWifiStatus(t *testing.T) {
	w := *iface(wifiSnap(), "wlp3s0").Wifi
	if got := wifiStatus(&w, "es"); got != `Conectado a "Casa" · 5 GHz · -52 dBm` {
		t.Errorf("es = %q", got)
	}
	for _, c := range []struct {
		radio, state, connecting string
		nets                     []model.WifiNet
		want                     string
	}{
		{"soft", "unavailable", "", nil, "Off (by software)"},
		{"hard", "unavailable", "", nil, "Off (by hardware)"},
		{"on", "disconnected", "", []model.WifiNet{{SSID: "Plaza libre", Signal: 30}}, "On, not connected"},
		{"on", "connecting (configuring)", "Oficina", nil, `Connecting to "Oficina"…`},
		{"on", "connecting (prepare)", "", nil, "Connecting…"},
		{"on", "unmanaged", "", nil, "Not managed by NetworkManager"},
	} {
		x := model.Wifi{Manager: "nm", Radio: c.radio, State: c.state, Connecting: c.connecting, Networks: c.nets}
		if got := wifiStatus(&x, "en"); got != c.want {
			t.Errorf("%+v: %q, want %q", c, got, c.want)
		}
	}
}

// Una placa que NetworkManager no maneja se trata como unavailable: sin lista
// de redes cercanas ni acciones de fila para ella (regla del controller).
func TestWifiUnmanaged(t *testing.T) {
	s := wifiSnap()
	iface(s, "wlp4s0").Wifi = &model.Wifi{Manager: "nm", Radio: "on", State: "unmanaged"}
	v := buildWifi(t, s, "en")
	g := findGroup(v.Blocks, "wlp4s0")
	if len(g.Blocks) != 2 || g.Blocks[1].Text != "Not managed by NetworkManager" {
		t.Fatalf("unmanaged wlp4s0 = %+v", g.Blocks)
	}
}

func TestWifiPendingAndWithoutNetworkManager(t *testing.T) {
	s := wifiSnap()
	iface(s, "wlp3s0").Wifi = nil // todavía no se leyó
	iface(s, "wlp4s0").Wifi.Manager = ""
	v := buildWifi(t, s, "es")
	for _, n := range []string{"wlp3s0", "wlp4s0"} {
		if g := findGroup(v.Blocks, n); g.Blocks[len(g.Blocks)-1].Text != "Buscando redes…" {
			t.Errorf("%s = %+v", n, g.Blocks)
		}
	}
	s = wifiSnap()
	iface(s, "wlp3s0").Wifi = &model.Wifi{SSID: "Casa", Manager: "none"}
	iface(s, "wlp4s0").Wifi.Manager = "none"
	v = buildWifi(t, s, "en")
	g := findGroup(v.Blocks, "wlp3s0")
	if last := g.Blocks[len(g.Blocks)-1]; last.Text != "Wi-Fi not available: NetworkManager is needed" || last.Level != "warn" {
		t.Fatalf("without NetworkManager = %+v", g.Blocks)
	}
	if kvValue(g.Blocks, "*", "SSID") != "Casa" {
		t.Fatalf("the card lost what it showed before: %+v", g.Blocks)
	}
	for _, a := range v.Actions {
		if strings.HasPrefix(a.ID, "wifi.") {
			t.Fatalf("Wi-Fi action without NetworkManager: %+v", a)
		}
	}
}

func TestWifiErrorNote(t *testing.T) {
	s := wifiSnap()
	iface(s, "wlp3s0").Wifi.Error = "NetworkManager is busy."
	g := findGroup(buildWifi(t, s, "en").Blocks, "wlp3s0")
	if len(g.Blocks) != 4 || g.Blocks[2].Text != "Could not read the networks (NetworkManager is busy.); showing the list from 18:30:00" ||
		g.Blocks[2].Level != "warn" || g.Blocks[3].Type != "table" {
		t.Fatalf("with an error = %+v", g.Blocks)
	}
	iface(s, "wlp3s0").Wifi.Scanned = time.Time{}
	iface(s, "wlp3s0").Wifi.Networks = nil
	g = findGroup(buildWifi(t, s, "en").Blocks, "wlp3s0")
	if g.Blocks[2].Text != "Could not read the networks: NetworkManager is busy." {
		t.Fatalf("error without a list = %+v", g.Blocks)
	}
}

func TestWifiEmptyAndOff(t *testing.T) {
	s := wifiSnap()
	w := iface(s, "wlp3s0").Wifi
	w.Networks, w.SSID, w.State = nil, "", "disconnected"
	v := buildWifi(t, s, "en")
	g := findGroup(v.Blocks, "wlp3s0")
	if last := g.Blocks[len(g.Blocks)-1]; last.Text != "No networks in range" {
		t.Fatalf("empty = %+v", g.Blocks)
	}
	for _, a := range v.Actions {
		if a.Target == "row" || a.ID == "wifi.band" {
			t.Fatalf("%s without networks or a connection", a.ID)
		}
	}
	w.Radio = "hard" // las dos apagadas por hardware: no hay nada que prender
	v = buildWifi(t, s, "en")
	if len(v.Actions) != 0 {
		t.Fatalf("all cards off by hardware: %+v", v.Actions)
	}
}

// hiddenSnap: wifiSnap con dos redes ocultas (una más fuerte que todas las
// con nombre) y una red con nombre que se escribe como un id de oculta.
func hiddenSnap() *model.Snapshot {
	s := wifiSnap()
	w := iface(s, "wlp3s0").Wifi
	w.Networks = append(w.Networks,
		model.WifiNet{Hidden: true, BSSID: "AA:BB:CC:00:00:05", Signal: 90, Band: "5", Security: "WPA2"},
		model.WifiNet{Hidden: true, BSSID: "AA:BB:CC:00:00:0D", Signal: 25, Band: "2.4"},
		model.WifiNet{SSID: "hidden:AA:BB:CC:00:00:09", Signal: 10, Band: "2.4"},
	)
	return s
}

// Las ocultas van después de todas las con nombre, por señal, con el nombre
// traducido, el flag hidden y el id "<placa>:hidden:<BSSID>".
func TestWifiHiddenRows(t *testing.T) {
	v := buildWifi(t, hiddenSnap(), "es")
	rows := tableRows(findGroup(v.Blocks, "wlp3s0").Blocks[2])
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	want := []string{"wlp3s0:Casa", "wlp3s0:Café: 2º piso", "wlp3s0:Lejana", "wlp3s0:Vecino 🚀", "wlp3s0:Plaza libre",
		"wlp3s0:hidden:AA:BB:CC:00:00:09", "wlp3s0:hidden:AA:BB:CC:00:00:05", "wlp3s0:hidden:AA:BB:CC:00:00:0D"}
	if !slices.Equal(ids, want) {
		t.Fatalf("ids = %q", ids)
	}
	h := rows[6]
	if !slices.Equal(h.Cells, []string{"(red oculta)", "90%", "5 GHz", "WPA2", ""}) || !slices.Contains(h.Flags, "hidden") {
		t.Fatalf("hidden row = %+v", h)
	}
	if rows[7].Cells[0] != "(red oculta)" || rows[7].Cells[3] != "abierta" {
		t.Fatalf("open hidden row = %+v", rows[7])
	}
	// Un SSID real que empieza con "hidden:" es una red con nombre.
	if named := rows[5]; named.Cells[0] != "hidden:AA:BB:CC:00:00:09" || slices.Contains(named.Flags, "hidden") {
		t.Fatalf("named row = %+v", named)
	}
	if got := buildWifi(t, hiddenSnap(), "en"); tableRows(findGroup(got.Blocks, "wlp3s0").Blocks[2])[6].Cells[0] != "(hidden network)" {
		t.Fatal("english name")
	}
}

// En una oculta la única acción es conectar (wifi.connect_hidden, con c); en
// las con nombre, connect_hidden no aparece.
func TestWifiHiddenRowActions(t *testing.T) {
	s := hiddenSnap()
	v := buildWifi(t, s, "en")
	rows := tableRows(findGroup(v.Blocks, "wlp3s0").Blocks[2])
	want := map[string][]string{
		"wlp3s0:Casa":                     {"wifi.disconnect", "wifi.forget"},
		"wlp3s0:Café: 2º piso":            {"wifi.connect", "wifi.forget"},
		"wlp3s0:Lejana":                   {"wifi.connect_new"},
		"wlp3s0:Vecino 🚀":                 {"wifi.connect_new"},
		"wlp3s0:Plaza libre":              {"wifi.connect"},
		"wlp3s0:hidden:AA:BB:CC:00:00:09": {"wifi.connect"},
		"wlp3s0:hidden:AA:BB:CC:00:00:05": {"wifi.connect_hidden"},
		"wlp3s0:hidden:AA:BB:CC:00:00:0D": {"wifi.connect_hidden"},
	}
	for _, r := range rows {
		if got := rowActions(v.Actions, r); !slices.Equal(got, want[r.ID]) {
			t.Errorf("%s: %q, want %q (flags %q)", r.ID, got, want[r.ID], r.Flags)
		}
	}
	a := actionByID(v.Actions, "wifi.connect_hidden")
	if a == nil || a.Key != "c" || a.Target != "row" || a.Mode != "background" || a.Label != "Connect to a hidden network" {
		t.Fatalf("connect_hidden = %+v", a)
	}
}

// Una oculta conectada sigue oculta en la tabla (no se inventa el nombre),
// con estado conectada y solo d (desconectar usa la placa, no el nombre); la línea de estado sale del nombre de
// la conexión, como siempre.
func TestWifiHiddenConnected(t *testing.T) {
	s := hiddenSnap()
	w := iface(s, "wlp3s0").Wifi
	w.SSID = "Escondida"
	for i := range w.Networks {
		w.Networks[i].InUse = w.Networks[i].BSSID == "AA:BB:CC:00:00:05"
	}
	v := buildWifi(t, s, "en")
	g := findGroup(v.Blocks, "wlp3s0")
	if g.Blocks[1].Text != `Connected to "Escondida" · 5 GHz · -52 dBm` {
		t.Fatalf("status = %q", g.Blocks[1].Text)
	}
	rows := tableRows(g.Blocks[2])
	h := rows[len(rows)-2]
	if h.ID != "wlp3s0:hidden:AA:BB:CC:00:00:05" || h.Cells[0] != "(hidden network)" || h.Cells[4] != "connected" {
		t.Fatalf("connected hidden row = %+v", h)
	}
	if got := rowActions(v.Actions, h); !slices.Equal(got, []string{"wifi.disconnect"}) {
		t.Fatalf("actions on the connected hidden row: %q", got)
	}
}
