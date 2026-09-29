package wifi

import (
	"context"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

var fixtureNets = []model.WifiNet{
	{SSID: "Casa", Signal: 78, Band: "5", Security: "WPA2", Saved: true, UUID: "5d6f7a8b-0000-4000-8000-000000000002", InUse: true},
	{Hidden: true, BSSID: "AA:BB:CC:00:00:05", Signal: 62, Band: "5", Security: "WPA2"},
	{SSID: "Café: 2º piso", Signal: 55, Band: "2.4", Security: "WPA2", Saved: true, UUID: "8c9d0e1f-0000-4000-8000-000000000003"},
	{SSID: "Vecino 🚀", Signal: 48, Band: "6", Security: "WPA3"},
	{SSID: "Plaza libre", Signal: 30, Band: "2.4"},
	{SSID: `$(reboot); "x"`, Signal: 20, Band: "2.4", Security: "WPA2"},
}

// La guardada "Oficina 2" es la red "Café: 2º piso": el nombre de la
// conexión no es el SSID.
func TestRead(t *testing.T) {
	env := source.Fixture(t, "wifi")
	var r Reader
	sc := r.Read(env, []string{"wlp3s0", "wlp4s0"}, source.FixtureNow)
	if sc.Manager != "nm" || !sc.Enabled || sc.Err != "" || !sc.At.Equal(source.FixtureNow) {
		t.Fatalf("scan = %+v", sc)
	}
	w3, w4 := sc.Ifaces["wlp3s0"], sc.Ifaces["wlp4s0"]
	if w3.Radio != "on" || w3.State != "connected" || w3.Connecting != "" || !slices.Equal(w3.Networks, fixtureNets) {
		t.Fatalf("wlp3s0 = %+v", w3)
	}
	if w4.Radio != "hard" || w4.State != "unavailable" || w4.Networks != nil {
		t.Fatalf("wlp4s0 = %+v", w4)
	}
	// wlp4s0 está apagada por hardware: no se le pide la lista.
	if n := runs(env).Count(source.Key("nmcli", "-t", "-f", listFields, "device", "wifi", "list", "ifname", "wlp4s0")); n != 0 {
		t.Fatalf("list asked for a card that is off: %d", n)
	}
}

func TestReadCachesSavedSSIDs(t *testing.T) {
	env := source.Fixture(t, "wifi")
	var r Reader
	r.Read(env, []string{"wlp3s0"}, source.FixtureNow)
	r.Read(env, []string{"wlp3s0"}, source.FixtureNow.Add(10*time.Second))
	fr := runs(env)
	if n := fr.Count(source.Key("nmcli", "-e", "no", "-g", "802-11-wireless.ssid", "connection", "show")); n != 2 {
		t.Errorf("SSID of saved connections asked %d times, want 2 (one per connection, then cached)", n)
	}
	if n := fr.Count(source.Key("nmcli", "-t", "-f", "NAME,UUID,TYPE", "connection", "show")); n != 2 {
		t.Errorf("saved connections listed %d times, want 2 (every read)", n)
	}
}

func TestReadConnecting(t *testing.T) {
	env := tempWifi(t)
	put(t, env, "cmd/nmcli_-t_-f_DEVICE,STATE,CONNECTION_device.txt", "wlp3s0:connecting (configuring):Oficina 2\nwlp4s0:unavailable:\n")
	var r Reader
	w := r.Read(env, []string{"wlp3s0"}, source.FixtureNow).Ifaces["wlp3s0"]
	if w.State != "connecting (configuring)" || w.Connecting != "Café: 2º piso" {
		t.Fatalf("wlp3s0 = %+v", w)
	}
}

func TestReadWithoutNetworkManager(t *testing.T) {
	env := source.Fixture(t, "wifi")
	env.Run = &source.FakeRunner{Dir: t.TempDir()} // sin nmcli
	var r Reader
	if sc := r.Read(env, []string{"wlp3s0"}, source.FixtureNow); sc.Manager != "none" || sc.Err != "" {
		t.Fatalf("without nmcli = %+v", sc)
	}
	if n := runs(env).Count("nmcli"); n != 1 {
		t.Fatalf("nmcli ran %d times, want 1", n)
	}
	env = tempWifi(t)
	put(t, env, "cmd/nmcli_-t_radio_wifi.txt", "Error: NetworkManager is not running.\n")
	put(t, env, "cmd/nmcli_-t_radio_wifi.exit", "8\n")
	if sc := r.Read(env, []string{"wlp3s0"}, source.FixtureNow); sc.Manager != "none" {
		t.Fatalf("NetworkManager stopped = %+v", sc)
	}
}

// Una lectura que falla deja la última lista buena, con su hora, y la nota.
func TestReadFailureKeepsLastList(t *testing.T) {
	env := tempWifi(t)
	var r Reader
	first := r.Read(env, []string{"wlp3s0"}, source.FixtureNow)
	list := "cmd/nmcli_-t_-f_IN-USE,SSID,SIGNAL,FREQ,SECURITY,BSSID_device_wifi_list_ifname_wlp3s0_--rescan_no"
	put(t, env, list+".txt", "Error: NetworkManager is busy.\n")
	put(t, env, list+".exit", "1\n")
	sc := r.Read(env, []string{"wlp3s0"}, source.FixtureNow.Add(10*time.Second))
	if sc.Err != "NetworkManager is busy." || !sc.At.Equal(first.At) || !slices.Equal(sc.Ifaces["wlp3s0"].Networks, fixtureNets) {
		t.Fatalf("failed read = %+v", sc)
	}
	var fresh Reader
	sc = fresh.Read(env, []string{"wlp3s0"}, source.FixtureNow)
	// Sin lista previa, el radio sale de rfkill (wlp3s0 no está bloqueada).
	if sc.Manager != "nm" || sc.Err != "NetworkManager is busy." || !sc.At.IsZero() ||
		len(sc.Ifaces) != 1 || sc.Ifaces["wlp3s0"].Radio != "on" {
		t.Fatalf("failed first read = %+v", sc)
	}
}

// Si la primera lectura falla (sin lista previa), la placa apagada por
// rfkill sigue mostrando su estado real: no hace falta nmcli para eso.
func TestReadFirstFailureUsesRfkill(t *testing.T) {
	env := tempWifi(t)
	put(t, env, "cmd/nmcli_-t_radio_wifi.txt", "Error: timed out.\n")
	put(t, env, "cmd/nmcli_-t_radio_wifi.exit", "1\n")
	put(t, env, "root/sys/class/rfkill/rfkill1/soft", "1\n") // wlp3s0 (phy0) apagada por software
	var r Reader
	sc := r.Read(env, []string{"wlp3s0"}, source.FixtureNow)
	if sc.Err == "" || len(sc.Ifaces) != 1 {
		t.Fatalf("failed first read = %+v", sc)
	}
	if w := sc.Ifaces["wlp3s0"]; w.Radio != "soft" {
		t.Fatalf("wlp3s0.Radio = %q, want %q", w.Radio, "soft")
	}
}

func TestMessage(t *testing.T) {
	if m := Message("es", "nmcli", nil, context.DeadlineExceeded); m != "no respondió a tiempo" {
		t.Errorf("timeout = %q", m)
	}
	if m := Message("en", "nmcli", nil, &exec.Error{Name: "nmcli", Err: exec.ErrNotFound}); m != "requires nmcli" {
		t.Errorf("not found = %q", m)
	}
	if m := Message("en", "nmcli", []byte("ignored\n"), &exec.ExitError{Stderr: []byte("\nError: Connection activation failed: Secrets were required.\nmore\n")}); m != "Connection activation failed: Secrets were required." {
		t.Errorf("stderr = %q", m)
	}
	long := strings.Repeat("x", 300)
	if m := Message("en", "nmcli", []byte(long), &source.ExitCode{Code: 1}); len([]rune(m)) != 120 || !strings.HasSuffix(m, "…") {
		t.Errorf("long = %d runes", len([]rune(m)))
	}
}

func TestApply(t *testing.T) {
	n := &model.Network{Interfaces: []model.Iface{
		{Name: "enp5s0", Kind: "ethernet"},
		{Name: "wlp3s0", Kind: "wifi", Up: true, Wifi: &model.Wifi{SSID: "Casa"}},
		{Name: "wlp4s0", Kind: "wifi"},
	}}
	sc := &Scan{Manager: "nm", Enabled: true, At: source.FixtureNow, Ifaces: map[string]IfaceScan{
		"wlp3s0": {Radio: "on", State: "connected", Networks: fixtureNets},
		"wlp4s0": {Radio: "hard", State: "unavailable"},
	}}
	Apply(n, sc)
	w3, w4 := n.Interfaces[1].Wifi, n.Interfaces[2].Wifi
	if n.Interfaces[0].Wifi != nil || w3.SSID != "Casa" || w3.Manager != "nm" || w3.Radio != "on" || len(w3.Networks) != len(fixtureNets) ||
		!w3.Scanned.Equal(source.FixtureNow) || w4 == nil || w4.Radio != "hard" {
		t.Fatalf("applied = %+v / %+v", w3, w4)
	}
	Apply(n, nil)
	if w3.SSID != "Casa" || w3.Manager != "" || w3.Radio != "" || w3.Networks != nil || !w3.Scanned.IsZero() {
		t.Fatalf("cleared = %+v", w3)
	}
}
