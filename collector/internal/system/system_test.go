package system

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

func TestStaticThreadripper(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	r := &collect.Report{Lang: "en"}
	s := Static(env, r)
	if s.Board != (model.DMIBoard{Vendor: "ASUSTeK COMPUTER INC.", Name: "ROG ZENITH EXTREME", Version: "Rev 1.xx"}) {
		t.Errorf("board = %+v", s.Board)
	}
	if s.Product != (model.DMIProduct{}) {
		t.Errorf("product must be empty (placeholders): %+v", s.Product)
	}
	if s.BIOS != (model.DMIBIOS{Vendor: "American Megatrends Inc.", Version: "2001", Date: "07/31/2019", Release: "5.13"}) {
		t.Errorf("bios = %+v", s.BIOS)
	}
	if s.Chassis != "Desktop" || s.OS != "Omarchy" || s.Omarchy != "4.0.4-1" || s.Kernel != "7.2.5-3-omarchy" || s.Hostname != "host" {
		t.Errorf("chassis/os/omarchy/kernel/host = %q %q %q %q %q", s.Chassis, s.OS, s.Omarchy, s.Kernel, s.Hostname)
	}
	if s.Packages.Pacman != 4 || s.Packages.Flatpak != 2 {
		t.Errorf("packages = %+v", s.Packages)
	}
	want := &model.BootTiming{FirmwareS: 35.174, LoaderS: 5.917, KernelS: 80.26, UserspaceS: 4.541, TotalS: 125.894}
	if !reflect.DeepEqual(s.Boot, want) {
		t.Errorf("boot = %+v", s.Boot)
	}
	if !reflect.DeepEqual(s.Audio, []string{"HD-Audio Generic", "HDA NVidia"}) {
		t.Errorf("audio = %v", s.Audio)
	}
	if s.SecureBoot == nil || *s.SecureBoot || s.TPM != "" {
		t.Errorf("secure boot/tpm = %v %q", s.SecureBoot, s.TPM)
	}
	if len(s.PCI) != 2 || s.PCI[0] != (model.PCIDevice{Slot: "0000:42:00.0", Class: "Display", Vendor: "NVIDIA Corporation", Device: "GA102 [GeForce RTX 3080]", VendorID: "10de", DeviceID: "2206", Driver: "nvidia"}) ||
		s.PCI[1].Class != "Multimedia" || s.PCI[1].Driver != "snd_hda_intel" {
		t.Errorf("pci (bridges excluded) = %+v", s.PCI)
	}
	if len(s.USB) != 2 || s.USB[0].Path != "1-11" || s.USB[0].Product != "AURA Custom Human interface" || s.USB[0].Vendor != "AsusTek Computer Inc." ||
		s.USB[1] != (model.USBDevice{Path: "1-5", Vendor: "Logitech, Inc.", Product: "Unifying Receiver", VendorID: "046d", ProductID: "c52b", Driver: "usbhid", SpeedMbps: 12}) {
		t.Errorf("usb (root hubs excluded) = %+v", s.USB)
	}
	if len(s.Monitors) != 2 {
		t.Fatalf("monitors = %+v", s.Monitors)
	}
	m := s.Monitors[0]
	if m != (model.Monitor{Connector: "DP-1", Manufacturer: "DEL", Model: "DELL S2721DGF", Serial: "SERIAL7", Connected: true, WidthMM: 600, HeightMM: 340, Preferred: "2560x1440@143.91"}) {
		t.Errorf("monitor = %+v", m)
	}
	if s.Monitors[1].Connector != "HDMI-A-1" || s.Monitors[1].Connected || s.Monitors[1].Model != "" {
		t.Errorf("disconnected monitor = %+v", s.Monitors[1])
	}
}

func TestSlowThreadripper(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	r := &collect.Report{Lang: "en"}
	s := Static(env, r)
	Slow(env, s, r)
	if len(s.FailedUnits) != 1 || s.FailedUnits[0] != (model.Unit{Name: "nvidia-powerd.service", Scope: "system", Description: "NVIDIA Dynamic Boost Daemon"}) {
		t.Errorf("failed units = %+v", s.FailedUnits)
	}
	if s.UptimeS != 273600.5 || s.BootTime != source.FixtureNow.Add(-273600500000000) {
		t.Errorf("uptime/boot time = %v %v", s.UptimeS, s.BootTime)
	}
	Foreign(env, s, r)
	if s.Packages.Foreign == nil || *s.Packages.Foreign != 3 {
		t.Errorf("foreign = %v", s.Packages.Foreign)
	}
}

func fwEnv(t *testing.T, out string, exit int) source.Env {
	t.Helper()
	dir := t.TempDir()
	k := source.Key("fwupdmgr", "get-updates", "--json")
	os.WriteFile(filepath.Join(dir, k+".txt"), []byte(out), 0o644)
	if exit != 0 {
		os.WriteFile(filepath.Join(dir, k+".exit"), []byte("2\n"), 0o644)
	}
	return source.Env{Run: &source.FakeRunner{Dir: dir}, Now: func() time.Time { return source.FixtureNow }, Lang: "en"}
}

func TestFirmware(t *testing.T) {
	env := fwEnv(t, `{"Devices":[{"Name":"UEFI dbx","Vendor":"Microsoft","Version":"371","Releases":[{"Version":"377"}]}]}`, 0)
	r := &collect.Report{Lang: "en"}
	f := Firmware(env, r)
	if f == nil || len(f.Devices) != 1 || f.Devices[0] != (model.FirmwareDevice{Name: "UEFI dbx", Current: "371", Available: "377", Vendor: "Microsoft"}) || !f.Checked.Equal(source.FixtureNow) {
		t.Fatalf("firmware = %+v", f)
	}
	env = fwEnv(t, "No updatable devices\n", 2)
	f = Firmware(env, r)
	if f == nil || len(f.Devices) != 0 || len(r.List) != 0 {
		t.Fatalf("no updates = %+v, unavailable %+v", f, r.List)
	}
	env = fwEnv(t, "", 0)
	env.Run = &source.FakeRunner{Dir: t.TempDir()}
	if f := Firmware(env, r); f != nil || len(r.List) != 1 || r.List[0].Reason != "missing_tool" {
		t.Fatalf("no fwupdmgr = %+v %+v", f, r.List)
	}
}

func TestMinimal(t *testing.T) {
	env := source.Fixture(t, "minimal")
	r := &collect.Report{Lang: "en"}
	s := Static(env, r)
	Slow(env, s, r)
	Foreign(env, s, r)
	if s.Chassis != "" || s.Omarchy != "" || s.Boot != nil || s.SecureBoot != nil || s.Packages.Foreign != nil {
		t.Errorf("invented values: %+v", s)
	}
	found := map[string]bool{}
	for _, u := range r.List {
		found[u.What] = true
	}
	for _, w := range []string{"system.boot", "system.failed_units", "system.packages"} {
		if !found[w] {
			t.Errorf("missing unavailable %s in %+v", w, r.List)
		}
	}
}

func TestSlowRefreshesUSBAndOmarchy(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	s := &model.System{}
	Slow(env, s, &collect.Report{Lang: "en"})
	if len(s.USB) == 0 || s.Omarchy != "4.0.4-1" {
		t.Fatalf("slow read must refresh hot-plugged USB and the Omarchy version: usb %d, omarchy %q", len(s.USB), s.Omarchy)
	}
}

func TestEDIDEndingInNewline(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(env.FS.Root)); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "sys/class/drm/card1-DP-1/edid")
	e, _ := os.ReadFile(p)
	e[16] += e[127] - 0x0a // la semana de fabricación: el checksum pasa a ser '\n'
	e[127] = 0x0a
	os.Chmod(p, 0o644)
	os.WriteFile(p, e, 0o644)
	env.FS.Root = root
	m := monitors(env)
	if len(m) == 0 || m[0].Model != "DELL S2721DGF" {
		t.Fatalf("an EDID whose checksum byte is 0x0A must still parse: %+v", m)
	}
}
