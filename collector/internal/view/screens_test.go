package view

import (
	"slices"
	"strings"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

func linksTo(bs []Block, section string) []string {
	var out []string
	for _, b := range bs {
		if b.Link == section {
			out = append(out, b.Text)
		}
	}
	return out
}

// La máquina de referencia: sin aviso (49.99 es lo único a 3440×1440) y con
// la línea informativa de HDMI (el EDID declara hasta 75 Hz).
func TestDevicesReference(t *testing.T) {
	s, _ := snap(t, "ultrawide")
	v, err := Build("devices", s, Options{Lang: "en"})
	if err != nil {
		t.Fatal(err)
	}
	m := table(v, "Monitors")
	if m == nil || !slices.Equal(m.Columns, []string{"Connector", "Model", "Size", "In use", "Max at this resolution", "VRR", "Color"}) {
		t.Fatalf("monitors = %+v", m)
	}
	want := []string{"HDMI-A-1", "S34J55x", "80 × 33 cm", "3440×1440 @ 49.99 Hz", "49.99 Hz", "off", "srgb · XRGB8888"}
	if got := cellsOf(m); len(got) != 1 || !slices.Equal(got[0], want) {
		t.Fatalf("row = %q", got)
	}
	note := "HDMI-A-1: the monitor declares up to 75 Hz, but over HDMI it offers 49.99 Hz at 3440×1440. Another input (DisplayPort) or a lower resolution may allow more."
	if lvl, ok := texts(v)[note]; !ok || lvl != "" {
		t.Fatalf("texts = %v", texts(v))
	}
	if w := linksTo(Warnings(s, Options{Lang: "en"}), "devices"); len(w) != 0 {
		t.Fatalf("warnings = %q", w)
	}
	u := table(v, "USB")
	if u == nil || !slices.Equal(u.Columns, []string{"Port", "Device", "USB", "Speed", "Driver"}) ||
		!slices.Equal(cellsOf(u)[0], []string{"1-5", "Logitech USB Receiver", "2.00", "12 Mb/s", "—"}) {
		t.Fatalf("usb = %+v", u)
	}
	noEmptyValues(t, v)
}

// Sin Hyprland (threadripper): la tabla de hoy, con el modo preferido.
func TestDevicesWithoutHyprland(t *testing.T) {
	v := build(t, "devices", "threadripper", Options{})
	m := table(v, "Monitors")
	if m == nil || !slices.Equal(m.Columns, []string{"Connector", "Model", "Size", "Mode"}) ||
		!slices.Equal(cellsOf(m)[0], []string{"DP-1", "DELL S2721DGF", "60 × 34 cm", "2560x1440@143.91"}) {
		t.Fatalf("monitors = %+v", m)
	}
}

// Review Focus 3 (vista): el aviso solo con 1 Hz o más de diferencia, en un
// monitor activo que no espeja; nunca con refreshRate 0.
func TestMonitorWarnings(t *testing.T) {
	mode := func(hz, best float64) *model.MonitorMode {
		return &model.MonitorMode{Width: 2560, Height: 1440, RefreshHz: hz, Scale: 1, BestHz: best}
	}
	y := &model.System{Monitors: []model.Monitor{
		{Connector: "DP-1", Connected: true, Model: "A", Live: mode(59.951, 143.91)}, // aviso
		{Connector: "DP-2", Connected: true, Model: "B", Live: mode(59.94, 60)},      // 0.06 Hz: no
		{Connector: "DP-3", Connected: true, Model: "C", Live: &model.MonitorMode{Width: 2560, Height: 1440, RefreshHz: 60, BestHz: 144, MirrorOf: "DP-1"}},
		{Connector: "DP-4", Connected: true, Model: "D", Live: &model.MonitorMode{Width: 2560, Height: 1440, RefreshHz: 60, BestHz: 144, Disabled: true}},
		{Connector: "DP-5", Connected: true, Model: "E", Live: mode(0, 144)},
		{Connector: "DP-6", Connected: false, Live: mode(60, 144)},
		{Connector: "DP-7", Connected: true, Model: "F"}, // sin Live
	}}
	w := monitorWarnings(y, "en")
	if !slices.Equal(w, []string{"DP-1 runs at 59.95 Hz; it offers 143.91 Hz at 2560×1440"}) {
		t.Fatalf("warnings = %q", w)
	}
	if es := monitorWarnings(y, "es"); es[0] != "DP-1 va a 59.95 Hz; ofrece 143.91 Hz a 2560×1440" {
		t.Errorf("es = %q", es)
	}
	s := &model.Snapshot{Lang: "en", System: y}
	v, _ := Build("devices", s, Options{Lang: "en"})
	if lvl, ok := texts(v)[w[0]]; !ok || lvl != "warn" {
		t.Errorf("the warning is also in the view: %v", texts(v))
	}
	rows := cellsOf(table(v, "Monitors"))
	if rows[2][3] != "Mirrors DP-1" || rows[3][3] != "Off" || rows[5][3] != "—" || rows[0][4] != "143.91 Hz" {
		t.Errorf("rows = %q", rows)
	}
	y.Monitors[0].Live.Scale = 1.3333333
	if got := cellsOf(table(buildView(t, "devices", s), "Monitors"))[0][3]; got != "2560×1440 @ 59.95 Hz ×1.33" {
		t.Errorf("scaled = %q", got)
	}
}

func buildView(t *testing.T, id string, s *model.Snapshot) *View {
	t.Helper()
	v, err := Build(id, s, Options{Lang: s.Lang})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// Review Focus 5 (vista): el aviso solo es seguro con bcdUSB ≥ 3.00 a ≤ 480;
// un 2.10 de almacenamiento en un puerto con par SuperSpeed es una nota.
func TestUSBWarnings(t *testing.T) {
	y := &model.System{USB: []model.USBDevice{
		{Path: "2-1", Vendor: "SanDisk", Product: "Ultra", Version: "3.20", SpeedMbps: 480, Class: "08"}, // aviso
		{Path: "1-3", Product: "Stick", Version: "2.10", SpeedMbps: 480, Class: "08", SSPort: true},      // nota
		{Path: "1-4", Product: "Cam", Version: "2.00", SpeedMbps: 480, Class: "0e"},                      // sin peer: nada
		{Path: "1-6", Product: "Hub", Version: "2.10", SpeedMbps: 480, Class: "09", SSPort: true},        // hub: nada
		{Path: "2-2", Product: "Fast", Version: "3.10", SpeedMbps: 5000, Class: "08"},                    // bien
		{Path: "1-7", Version: "garbage", SpeedMbps: 480, Class: "08", SSPort: true},                     // nota, por el puerto
		{Path: "1-8", Version: "3.00", Class: "08"},                                                      // sin velocidad: nada
	}}
	if w := usbWarnings(y, "en"); !slices.Equal(w, []string{"SanDisk Ultra is a USB 3 device running at USB 2 speed"}) {
		t.Fatalf("warnings = %q", w)
	}
	want := []string{"Stick: USB 2 speed on a USB 3 port (if the device is USB 3, check the cable)",
		"1-7: USB 2 speed on a USB 3 port (if the device is USB 3, check the cable)"}
	if n := usbNotes(y, "en"); !slices.Equal(n, want) {
		t.Fatalf("notes = %q", n)
	}
	s := &model.Snapshot{Lang: "en", System: y}
	v := buildView(t, "devices", s)
	tx := texts(v)
	if lvl, ok := tx["SanDisk Ultra is a USB 3 device running at USB 2 speed"]; !ok || lvl != "warn" {
		t.Fatalf("texts = %v", tx)
	}
	if lvl, ok := tx[want[0]]; !ok || lvl != "" {
		t.Fatalf("texts = %v", tx)
	}
	if got := cellsOf(table(v, "USB")); got[0][2] != "3.20" || got[5][2] != "garbage" || !strings.HasSuffix(got[4][3], "Mb/s") {
		t.Fatalf("rows = %q", got)
	}
}

// Una sola fuente de verdad: Resumen y badge con las mismas funciones.
func TestDevicesWarningsAndBadge(t *testing.T) {
	y := &model.System{FailedUnits: []model.Unit{},
		Monitors: []model.Monitor{{Connector: "DP-1", Connected: true,
			Live: &model.MonitorMode{Width: 2560, Height: 1440, RefreshHz: 59.95, Scale: 1, BestHz: 143.91}}},
		USB: []model.USBDevice{{Path: "2-1", Product: "Ultra", Version: "3.20", SpeedMbps: 480, Class: "08"}}}
	s := &model.Snapshot{Lang: "en", System: y}
	w := linksTo(Warnings(s, Options{Lang: "en"}), "devices")
	if len(w) != 2 {
		t.Fatalf("warnings = %q", w)
	}
	for _, sec := range Sections(s, Options{Lang: "en"}) {
		if sec.ID == "devices" && sec.Badge != 2 {
			t.Fatalf("badge = %d", sec.Badge)
		}
	}
}
