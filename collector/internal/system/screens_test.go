package system

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// samsungEDID es el bloque base del EDID del Samsung S34J55x de la máquina
// de referencia (HDMI-A-1, 2026-09-27), con el serial anonimizado como lo
// hace la captura. Rango 0xFD en el offset 72: 50–75 Hz V, 30–81 kHz H,
// 270 MHz.
const samsungEDID = "00ffffffffffff004c2d720f00000000271e0103805021782a46c5a5564f9b250f5054bfef80714f810081c081809500a9c0b30001019d6770a0d0a0225030203a001d4d3100001a000000fd00324b1e511b000a202020202020000000fc005333344a3535780a2020202020000000ff0053455249414c0a20202020202000ec"

func samsung(t *testing.T) []byte {
	t.Helper()
	b, err := hex.DecodeString(samsungEDID)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Review Focus 4: el rango solo cuenta si describe al monitor.
func TestEDIDRange(t *testing.T) {
	var m model.Monitor
	parseEDID(samsung(t), &m)
	if m.Manufacturer != "SAM" || m.Model != "S34J55x" || m.Preferred != "3440x1440@49.99" || m.MaxVHz != 75 ||
		m.WidthMM != 800 || m.HeightMM != 330 {
		t.Fatalf("samsung = %+v", m)
	}
	for _, c := range []struct {
		name string
		edit func(e []byte)
		want int
	}{
		{"EDID 1.4: +255 to the maximum", func(e []byte) { e[72+4], e[72+6] = 0x02, 0x10 }, 271},
		{"minimum above maximum", func(e []byte) { e[72+5], e[72+6] = 80, 60 }, 0},
		{"maximum below the preferred mode", func(e []byte) { e[72+6] = 40 }, 0},
		{"maximum zero", func(e []byte) { e[72+5], e[72+6] = 0, 0 }, 0},
		{"no range descriptor", func(e []byte) { e[72+3] = 0x10 }, 0},
	} {
		e := samsung(t)
		c.edit(e)
		m := model.Monitor{}
		parseEDID(e, &m)
		if m.MaxVHz != c.want || m.Preferred != "3440x1440@49.99" {
			t.Errorf("%s: max %d, preferred %q", c.name, m.MaxVHz, m.Preferred)
		}
	}
	// El DELL del fixture threadripper trae un 0xFD de relleno: máximo 32 Hz
	// para un modo preferido de 143.91 Hz. Se ignora.
	env := source.Fixture(t, "threadripper")
	b, err := env.FS.Bytes("/sys/class/drm/card1-DP-1/edid")
	if err != nil {
		t.Fatal(err)
	}
	m = model.Monitor{}
	parseEDID(b, &m)
	if m.MaxVHz != 0 || m.Preferred != "2560x1440@143.91" || m.Model != "DELL S2721DGF" {
		t.Fatalf("dell = %+v", m)
	}
	// Un EDID corto no entra en pánico.
	m = model.Monitor{}
	parseEDID(samsung(t)[:100], &m)
	if m != (model.Monitor{}) {
		t.Fatalf("short = %+v", m)
	}
}

// usbEnv arma un Env sobre un directorio temporal con esos archivos.
func usbEnv(t *testing.T, files map[string]string) source.Env {
	t.Helper()
	root := t.TempDir()
	for p, s := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return source.Env{FS: source.FS{Root: root}, Lang: "en"}
}

// Review Focus 5 (lectura): versión con espacio, sin versión, basura; el
// puerto SuperSpeed por port/peer; la clase de la primera interfaz.
func TestUSBVersionPeerClass(t *testing.T) {
	env := usbEnv(t, map[string]string{
		"sys/bus/usb/devices/usb2/idVendor":               "1d6b\n",
		"sys/bus/usb/devices/1-3/idVendor":                "090c\n",
		"sys/bus/usb/devices/1-3/product":                 "Stick\n",
		"sys/bus/usb/devices/1-3/speed":                   "480\n",
		"sys/bus/usb/devices/1-3/version":                 " 2.10\n",
		"sys/bus/usb/devices/1-3/1-3:1.0/bInterfaceClass": "08\n",
		"sys/bus/usb/devices/1-3/port/peer.link":          "../../../usb2/2-0:1.0/usb2-port3\n",
		"sys/bus/usb/devices/1-4/idVendor":                "05e3\n",
		"sys/bus/usb/devices/1-4/product":                 "Hub\n",
		"sys/bus/usb/devices/1-4/speed":                   "480\n",
		"sys/bus/usb/devices/1-4/version":                 " 2.10\n",
		"sys/bus/usb/devices/1-7/idVendor":                "abcd\n",
		"sys/bus/usb/devices/1-7/version":                 "garbage\n",
		"sys/bus/usb/devices/2-1/idVendor":                "0781\n",
		"sys/bus/usb/devices/2-1/product":                 "Ultra\n",
		"sys/bus/usb/devices/2-1/speed":                   "5000\n",
		"sys/bus/usb/devices/2-1/version":                 " 3.20\n",
		"sys/bus/usb/devices/2-1/2-1:1.0/bInterfaceClass": "08\n",
	})
	got := usb(env)
	if len(got) != 4 {
		t.Fatalf("usb = %+v", got)
	}
	want := map[string][3]string{ // versión, clase, puerto SuperSpeed
		"1-3": {"2.10", "08", "true"}, "1-4": {"2.10", "", "false"}, "1-7": {"garbage", "", "false"}, "2-1": {"3.20", "08", "false"},
	}
	for _, d := range got {
		w := want[d.Path]
		ss := "false"
		if d.SSPort {
			ss = "true"
		}
		if d.Version != w[0] || d.Class != w[1] || ss != w[2] {
			t.Errorf("%s = version %q class %q ss %v", d.Path, d.Version, d.Class, d.SSPort)
		}
	}
}

// hyprEnv: un Hyprland que responde j/monitors con monitors ("" = no hay
// Hyprland).
func hyprEnv(t *testing.T, monitors string) source.Env {
	t.Helper()
	dir := t.TempDir()
	if monitors != "" {
		if err := os.WriteFile(filepath.Join(dir, "j-monitors.txt"), []byte(monitors), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return source.Env{FS: source.FS{Root: t.TempDir()}, Hypr: &source.FakeHypr{Dir: dir}, Lang: "en"}
}

// realMonitors es j/monitors de la máquina de referencia (Hyprland 0.56.2,
// 2026-09-27), recortado a los campos que se leen, con el serial
// anonimizado.
const realMonitors = `[{"id":0,"name":"HDMI-A-1","description":"Samsung Electric Company S34J55x SERIAL1","serial":"SERIAL1",
 "width":3440,"height":1440,"refreshRate":49.987,"scale":1,"vrr":false,"disabled":false,"currentFormat":"XRGB8888",
 "mirrorOf":"none","colorManagementPreset":"srgb",
 "availableModes":["3440x1440@49.99Hz","2560x1440@59.95Hz","2560x1080@60.00Hz","1920x1080@60.00Hz","1280x1024@75.03Hz"]}]`

func TestLiveReal(t *testing.T) {
	mons := []model.Monitor{{Connector: "HDMI-A-1", Connected: true}, {Connector: "DP-1"}}
	Live(hyprEnv(t, realMonitors), mons)
	want := model.MonitorMode{Width: 3440, Height: 1440, RefreshHz: 49.987, Scale: 1, Format: "XRGB8888", Preset: "srgb", BestHz: 49.99}
	if mons[0].Live == nil || *mons[0].Live != want || mons[1].Live != nil {
		t.Fatalf("live = %+v / %+v", mons[0].Live, mons[1].Live)
	}
}

// Review Focus 3: modos raros, espejo, apagado y un conector que no está.
func TestLiveModes(t *testing.T) {
	js := `[
	 {"name":"DP-1","width":2560,"height":1440,"refreshRate":59.951,"scale":1.25,"vrr":true,"mirrorOf":"none",
	  "availableModes":["2560x1440@143.91Hz","2560x1440@59.95Hz","1920x1080@240.00Hz","bad","2560x1440@","x@60Hz","2560x1440@-1Hz","2560x1440@165"]},
	 {"name":"HDMI-A-1","width":1920,"height":1080,"refreshRate":60,"mirrorOf":"DP-1","availableModes":["1920x1080@60.00Hz"]},
	 {"name":"DP-2","width":1920,"height":1080,"refreshRate":0,"disabled":true,"availableModes":[]},
	 {"name":"eDP-9","width":800,"height":600,"refreshRate":60}
	]`
	mons := []model.Monitor{{Connector: "DP-1", Connected: true}, {Connector: "HDMI-A-1", Connected: true}, {Connector: "DP-2", Connected: true}}
	Live(hyprEnv(t, js), mons)
	if l := mons[0].Live; l == nil || l.BestHz != 165 || l.Scale != 1.25 || !l.VRR || l.MirrorOf != "" {
		t.Errorf("DP-1 = %+v", l)
	}
	if l := mons[1].Live; l == nil || l.MirrorOf != "DP-1" || l.BestHz != 60 {
		t.Errorf("HDMI-A-1 = %+v", l)
	}
	if l := mons[2].Live; l == nil || !l.Disabled || l.BestHz != 0 {
		t.Errorf("DP-2 = %+v", l)
	}
	if got := bestHz([]string{"3440x1440@49.99Hz"}, 2560, 1440); got != 0 {
		t.Errorf("no mode at this resolution = %v", got)
	}
	if got := bestHz(nil, 1, 1); got != 0 {
		t.Errorf("no modes = %v", got)
	}
}

// Sin Hyprland, o con una respuesta rota, Live queda nil (también lo que
// había de antes).
func TestLiveWithoutHyprland(t *testing.T) {
	for _, env := range []source.Env{hyprEnv(t, ""), hyprEnv(t, "not json"), {Lang: "en"}} {
		mons := []model.Monitor{{Connector: "HDMI-A-1", Connected: true, Live: &model.MonitorMode{Width: 1}}}
		Live(env, mons)
		if mons[0].Live != nil {
			t.Errorf("live = %+v", mons[0].Live)
		}
	}
}

// M15 (ruling): sin ningún monitor conectado en mons (headless, como el
// fixture "apps"), Live no le pregunta nada a Hyprland, aunque haya una
// respuesta disponible.
func TestLiveNoConnectedMonitorsSkipsHyprland(t *testing.T) {
	dir := t.TempDir()
	hypr := &source.FakeHypr{Dir: dir}
	if err := os.WriteFile(filepath.Join(dir, "j-monitors.txt"), []byte(realMonitors), 0o644); err != nil {
		t.Fatal(err)
	}
	env := source.Env{FS: source.FS{Root: t.TempDir()}, Hypr: hypr, Lang: "en"}
	for _, mons := range [][]model.Monitor{
		nil,
		{{Connector: "HDMI-A-1", Connected: false, Live: &model.MonitorMode{Width: 1}}},
		{{Connector: "HDMI-A-1"}, {Connector: "DP-1"}},
	} {
		Live(env, mons)
		for _, m := range mons {
			if m.Live != nil {
				t.Errorf("live = %+v", m.Live)
			}
		}
	}
	if len(hypr.Calls) != 0 {
		t.Errorf("hypr calls = %v, want none (no monitor is connected)", hypr.Calls)
	}
}

// M2 (ruling): Format, Preset y MirrorOf vienen de Hyprland y el firmware
// puede haber metido secuencias ANSI/CSI o caracteres de formato Unicode
// (como el override RTL); se limpian con text.Clean antes de guardarlos.
func TestLiveCleansFormatPresetMirrorOf(t *testing.T) {
	js := `[{"name":"DP-1","width":1,"height":1,"currentFormat":"XRGB8888\u001b[31m","colorManagementPreset":"srgb‮","mirrorOf":"DP-2\u001b[0m"}]`
	mons := []model.Monitor{{Connector: "DP-1", Connected: true}}
	Live(hyprEnv(t, js), mons)
	l := mons[0].Live
	if l == nil || l.Format != "XRGB8888" || l.Preset != "srgb" || l.MirrorOf != "DP-2" {
		t.Fatalf("live = %+v", l)
	}
}

// M2 (minor): un monitor de Hyprland sin nombre no puede emparejar con un
// monitor sin Connector.
func TestLiveSkipsEmptyName(t *testing.T) {
	js := `[{"name":"","width":100,"height":100,"refreshRate":60}]`
	mons := []model.Monitor{{Connector: "", Connected: true}}
	Live(hyprEnv(t, js), mons)
	if mons[0].Live != nil {
		t.Errorf("live = %+v", mons[0].Live)
	}
}

// El fabricante y el producto vienen crudos de sysfs y llegan al Resumen:
// sin secuencias de escape ni caracteres de formato.
func TestUSBNamesAreCleaned(t *testing.T) {
	env := usbEnv(t, map[string]string{
		"sys/bus/usb/devices/1-3/idVendor":     "090c\n",
		"sys/bus/usb/devices/1-3/manufacturer": "\x1b[31mEvil‮ Corp\n",
		"sys/bus/usb/devices/1-3/product":      "Stick\x1b[0m‮\n",
	})
	got := usb(env)
	if len(got) != 1 || got[0].Vendor != "Evil Corp" || got[0].Product != "Stick" {
		t.Fatalf("usb = %+v", got)
	}
}
