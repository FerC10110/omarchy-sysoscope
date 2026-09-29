package wifi

import (
	"slices"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

func TestFields(t *testing.T) {
	for line, want := range map[string][]string{
		`*:Casa:78:5180 MHz:WPA2:AA\:BB\:CC\:00\:00\:01`: {"*", "Casa", "78", "5180 MHz", "WPA2", "AA:BB:CC:00:00:01"},
		` :Café\: 2º piso:55`:                            {" ", "Café: 2º piso", "55"},
		`C\:\\temp:x`:                                    {`C:\temp`, "x"},
		``:                                               {""},
	} {
		if got := Fields(line); !slices.Equal(got, want) {
			t.Errorf("Fields(%q) = %q, want %q", line, got, want)
		}
	}
}

func TestBandOf(t *testing.T) {
	for freq, want := range map[string]string{
		"2412 MHz": "2.4", "2499 MHz": "2.4", "2500 MHz": "", "4900 MHz": "5", "5924 MHz": "5",
		"5925 MHz": "6", "7124 MHz": "6", "7125 MHz": "", "": "", "abc": "",
	} {
		if got := BandOf(freq); got != want {
			t.Errorf("BandOf(%q) = %q, want %q", freq, got, want)
		}
	}
}

func TestSecurity(t *testing.T) {
	for raw, want := range map[string]string{
		"": "", "--": "", "WPA2": "WPA2", "WPA1 WPA2": "WPA2", "WPA3 WPA2": "WPA2 WPA3", "WEP": "WEP",
		"WPA2 802.1X": "802.1X", "WPA1": "WPA1", "OWE": "OWE", "WPA2 FOO": "WPA2 FOO",
	} {
		if got := Security(raw); got != want {
			t.Errorf("Security(%q) = %q, want %q", raw, got, want)
		}
	}
}

// Una red por SSID (la que está en uso o la de mejor señal), una oculta por
// BSSID, sin las líneas rotas, de mayor a menor señal.
func TestParseList(t *testing.T) {
	out := `*:Casa:78:5180 MHz:WPA2:AA\:BB\:CC\:00\:00\:01
 :Casa:81:2437 MHz:WPA2:AA\:BB\:CC\:00\:00\:02
 :Café\: 2º piso:55:2412 MHz:WPA1 WPA2:AA\:BB\:CC\:00\:00\:03
 :Plaza libre:30:2462 MHz::AA\:BB\:CC\:00\:00\:04
 ::62:5500 MHz:WPA2:AA\:BB\:CC\:00\:00\:05
 :Vecino 🚀:48:5975 MHz:WPA3:AA\:BB\:CC\:00\:00\:06
 :Vecino 🚀:52:5985 MHz:WPA3:AA\:BB\:CC\:00\:00\:08
 :C\:\\temp:20:2412 MHz:--:AA\:BB\:CC\:00\:00\:09
 :Mixta:40:5200 MHz:WPA2 WPA3:AA\:BB\:CC\:00\:00\:0A
 :Empresa:35:5220 MHz:WPA2 802.1X:AA\:BB\:CC\:00\:00\:0B
 :$(reboot); "x":19:2412 MHz:WPA2:AA\:BB\:CC\:00\:00\:07
rota:sin campos
 :Mala señal:abc:2412 MHz:WPA2:AA\:BB\:CC\:00\:00\:0C
`
	want := []model.WifiNet{
		{SSID: "Casa", Signal: 78, Band: "5", Security: "WPA2", InUse: true},
		{Hidden: true, BSSID: "AA:BB:CC:00:00:05", Signal: 62, Band: "5", Security: "WPA2"},
		{SSID: "Café: 2º piso", Signal: 55, Band: "2.4", Security: "WPA2"},
		{SSID: "Vecino 🚀", Signal: 52, Band: "6", Security: "WPA3"},
		{SSID: "Mixta", Signal: 40, Band: "5", Security: "WPA2 WPA3"},
		{SSID: "Empresa", Signal: 35, Band: "5", Security: "802.1X"},
		{SSID: "Plaza libre", Signal: 30, Band: "2.4"},
		{SSID: `C:\temp`, Signal: 20, Band: "2.4"},
		{SSID: `$(reboot); "x"`, Signal: 19, Band: "2.4", Security: "WPA2"},
	}
	if got := ParseList(out); !slices.Equal(got, want) {
		t.Fatalf("ParseList =\n%+v\nwant\n%+v", got, want)
	}
}

// Las ocultas (sin SSID) van una por BSSID, normalizado en mayúsculas: el
// mismo BSSID repetido es una sola (la en uso o la de mejor señal), BSSIDs
// distintos son filas distintas, y una en uso queda oculta pero en uso. Sin
// un BSSID válido no se puede identificar: se saltea.
func TestParseListHidden(t *testing.T) {
	out := ` ::40:2412 MHz:WPA2:aa\:bb\:cc\:dd\:ee\:01
 ::55:2437 MHz:WPA2:AA\:BB\:CC\:DD\:EE\:01
 ::30:5180 MHz::AA\:BB\:CC\:DD\:EE\:02
*::20:5200 MHz:WPA3:AA\:BB\:CC\:DD\:EE\:03
 ::70:5200 MHz:WPA3:AA\:BB\:CC\:DD\:EE\:03
 :Casa:50:5180 MHz:WPA2:AA\:BB\:CC\:DD\:EE\:04
 ::45:2412 MHz:WPA2:
 ::45:2412 MHz:WPA2:no-es-un-bssid
`
	want := []model.WifiNet{
		{Hidden: true, BSSID: "AA:BB:CC:DD:EE:01", Signal: 55, Band: "2.4", Security: "WPA2"},
		{SSID: "Casa", Signal: 50, Band: "5", Security: "WPA2"},
		{Hidden: true, BSSID: "AA:BB:CC:DD:EE:02", Signal: 30, Band: "5"},
		{Hidden: true, BSSID: "AA:BB:CC:DD:EE:03", Signal: 20, Band: "5", Security: "WPA3", InUse: true},
	}
	if got := ParseList(out); !slices.Equal(got, want) {
		t.Fatalf("ParseList =\n%+v\nwant\n%+v", got, want)
	}
}

func TestIsBSSID(t *testing.T) {
	for s, want := range map[string]bool{
		"AA:BB:CC:DD:EE:FF": true, "00:11:22:33:44:55": true, "aa:bb:cc:dd:ee:ff": false,
		"AA:BB:CC:DD:EE": false, "AA:BB:CC:DD:EE:FF:00": false, "AA-BB-CC-DD-EE-FF": false, "": false, "GG:BB:CC:DD:EE:FF": false,
	} {
		if got := IsBSSID(s); got != want {
			t.Errorf("IsBSSID(%q) = %v", s, got)
		}
	}
}

func TestParseListEmptyAndBroken(t *testing.T) {
	if got := ParseList(""); len(got) != 0 {
		t.Errorf("empty output = %+v", got)
	}
	if got := ParseList("rota\n:::\n*:x:NaN:2412 MHz:WPA2:y\n"); len(got) != 0 {
		t.Errorf("broken output = %+v", got)
	}
}

func TestParseConns(t *testing.T) {
	out := `Wired connection 1:0b1e0c9a-0000-4000-8000-000000000001:802-3-ethernet
Oficina\: 2:8c9d0e1f-0000-4000-8000-000000000003:802-11-wireless

roto
`
	want := []Conn{
		{Name: "Wired connection 1", UUID: "0b1e0c9a-0000-4000-8000-000000000001", Type: "802-3-ethernet"},
		{Name: "Oficina: 2", UUID: "8c9d0e1f-0000-4000-8000-000000000003", Type: "802-11-wireless"},
	}
	if got := ParseConns(out); !slices.Equal(got, want) {
		t.Fatalf("ParseConns = %+v", got)
	}
}

func TestParseDevices(t *testing.T) {
	out := "enp5s0:connected:Wired connection 1\nwlp3s0:connecting (configuring):Oficina 2\nwlp4s0:unavailable:\nlo:connected (externally):lo\n"
	d := ParseDevices(out)
	if d["wlp3s0"] != (Device{State: "connecting (configuring)", Conn: "Oficina 2"}) || d["wlp4s0"] != (Device{State: "unavailable"}) || len(d) != 4 {
		t.Fatalf("ParseDevices = %+v", d)
	}
}

func TestPrintable(t *testing.T) {
	if got := Printable("Casa\x1b[31m roja\n"); got != "Casa[31m roja" {
		t.Fatalf("Printable = %q", got)
	}
	if got := Printable("Vecino 🚀 ñ"); got != "Vecino 🚀 ñ" {
		t.Fatalf("Printable = %q", got)
	}
}
