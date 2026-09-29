package power

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// sleepEnv arma un Env sobre un directorio temporal con esos archivos (ruta
// del sistema → contenido).
func sleepEnv(t *testing.T, files map[string]string) source.Env {
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
	return source.Env{FS: source.FS{Root: root}, Lang: "en", Home: "/home/user"}
}

func TestMemSleep(t *testing.T) {
	for _, c := range []struct {
		file, mode string
		all        []string
	}{
		{"s2idle [deep]\n", "deep", []string{"s2idle", "deep"}},
		{"[s2idle] deep\n", "s2idle", []string{"s2idle", "deep"}},
		{"[s2idle]\n", "s2idle", []string{"s2idle"}},
		{"s2idle shallow deep\n", "", []string{"s2idle", "shallow", "deep"}}, // sin corchetes: ninguno en uso
	} {
		var p model.Power
		Sleep(sleepEnv(t, map[string]string{"sys/power/mem_sleep": c.file}), &p)
		if p.SleepMode != c.mode || !slices.Equal(p.SleepModes, c.all) {
			t.Errorf("%q: mode %q modes %q", c.file, p.SleepMode, p.SleepModes)
		}
	}
	var p model.Power
	Sleep(sleepEnv(t, nil), &p)
	if p.SleepMode != "" || p.SleepModes != nil || p.Wakeup != nil {
		t.Errorf("without the file: %+v", p)
	}
}

// Despiertan solo los USB hoja y las placas de red: ni hubs, ni raíces, ni
// puentes PCI, ni controladoras USB, ni lo que tiene wakeup = disabled.
func TestWakeup(t *testing.T) {
	env := sleepEnv(t, map[string]string{
		"sys/bus/usb/devices/usb1/power/wakeup":         "disabled\n",
		"sys/bus/usb/devices/usb1/bDeviceClass":         "09\n",
		"sys/bus/usb/devices/usb2/power/wakeup":         "enabled\n", // raíz aunque esté enabled
		"sys/bus/usb/devices/1-1/power/wakeup":          "enabled\n",
		"sys/bus/usb/devices/1-1/bDeviceClass":          "09\n", // un hub
		"sys/bus/usb/devices/1-1.2/power/wakeup":        "enabled\n",
		"sys/bus/usb/devices/1-1.2/bDeviceClass":        "00\n",
		"sys/bus/usb/devices/1-1.2/product":             "USB Receiver\x1b[31m\n",
		"sys/bus/usb/devices/1-1:1.0/power/wakeup":      "enabled\n", // interfaz
		"sys/bus/usb/devices/1-9/power/wakeup":          "enabled\n",
		"sys/bus/usb/devices/1-9/idVendor":              "abcd\n", // sin product ni usb.ids: el puerto
		"sys/bus/usb/devices/1-9/idProduct":             "0001\n",
		"sys/bus/usb/devices/1-11/power/wakeup":         "disabled\n",
		"sys/bus/usb/devices/1-11/product":              "AURA\n",
		"sys/bus/pci/devices/0000:00:01.1/class":        "0x060400\n",
		"sys/bus/pci/devices/0000:00:01.1/power/wakeup": "enabled\n",
		"sys/bus/pci/devices/0000:01:00.0/class":        "0x0c0330\n",
		"sys/bus/pci/devices/0000:01:00.0/power/wakeup": "enabled\n",
		"sys/bus/pci/devices/0000:05:00.0/class":        "0x020000\n",
		"sys/bus/pci/devices/0000:05:00.0/vendor":       "0x8086\n",
		"sys/bus/pci/devices/0000:05:00.0/device":       "0x1539\n",
		"sys/bus/pci/devices/0000:05:00.0/power/wakeup": "enabled\n",
		"sys/bus/pci/devices/0000:06:00.0/class":        "0x028000\n", // Wi-Fi, sin wakeup
		"sys/bus/pci/devices/0000:07:00.0/class":        "0x020000\n", // otra placa, sin pci.ids
		"sys/bus/pci/devices/0000:07:00.0/power/wakeup": "enabled\n",
		"usr/share/hwdata/pci.ids":                      "8086  Intel Corporation\n\t1539  I211 Gigabit Network Connection\n",
	})
	var p model.Power
	Sleep(env, &p)
	want := []model.WakeDevice{
		// M2: usbName limpia con text.Clean (CSI completo), no solo
		// wifi.Printable (que dejaría el "[31m" suelto tras sacar el ESC).
		{Name: "USB Receiver", Kind: "usb", Path: "1-1.2"},
		{Name: "1-9", Kind: "usb", Path: "1-9"},
		{Name: "Intel Corporation I211 Gigabit Network Connection", Kind: "net", Path: "0000:05:00.0"},
		{Name: "0000:07:00.0", Kind: "net", Path: "0000:07:00.0"},
	}
	if !slices.Equal(p.Wakeup, want) {
		t.Fatalf("wakeup = %+v", p.Wakeup)
	}
}
