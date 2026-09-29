package system

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
	"golang.org/x/sys/unix"
)

const (
	unitsKey     = "call org.freedesktop.systemd1 /org/freedesktop/systemd1 org.freedesktop.systemd1.Manager.ListUnitsByNames [systemd-timesyncd.service ufw.service firewalld.service nftables.service iptables.service ip6tables.service]"
	fileStateKey = "call org.freedesktop.systemd1 /org/freedesktop/systemd1 org.freedesktop.systemd1.Manager.GetUnitFileState systemd-timesyncd.service"
)

func TestClock(t *testing.T) {
	s := &model.System{}
	Slow(source.Fixture(t, "threadripper"), s, &collect.Report{Lang: "en"})
	want := &model.TimeSync{Zone: "America/Argentina/Buenos_Aires", NTP: true, Synced: true, CanNTP: true}
	if !reflect.DeepEqual(s.Time, want) {
		t.Fatalf("time = %+v", s.Time)
	}
}

// tzRoot arma un FS con /etc/localtime (symlink de verdad, archivo .link del
// fixture o archivo común) y /etc/timezone opcionales.
func tzRoot(t *testing.T, link, linkFile, regular, timezone string) source.FS {
	t.Helper()
	root := t.TempDir()
	etc := filepath.Join(root, "etc")
	os.MkdirAll(etc, 0o755)
	if link != "" {
		if err := os.Symlink(link, filepath.Join(etc, "localtime")); err != nil {
			t.Fatal(err)
		}
	}
	if linkFile != "" {
		os.WriteFile(filepath.Join(etc, "localtime.link"), []byte(linkFile+"\n"), 0o644)
	}
	if regular != "" {
		os.WriteFile(filepath.Join(etc, "localtime"), []byte(regular), 0o644)
	}
	if timezone != "" {
		os.WriteFile(filepath.Join(etc, "timezone"), []byte(timezone), 0o644)
	}
	return source.FS{Root: root}
}

func TestClockZone(t *testing.T) {
	for _, c := range []struct {
		name                              string
		link, linkFile, regular, timezone string
		want                              string
	}{
		{"absolute symlink", "/usr/share/zoneinfo/Europe/Madrid", "", "", "", "Europe/Madrid"},
		{"relative symlink", "../usr/share/zoneinfo/Asia/Tokyo", "", "", "Etc/Ignored\n", "Asia/Tokyo"},
		{"fixture .link", "", "/usr/share/zoneinfo/America/Argentina/Buenos_Aires", "", "", "America/Argentina/Buenos_Aires"},
		{"regular file, /etc/timezone", "", "", "TZif2...", "Europe/Berlin\n", "Europe/Berlin"},
		{"regular file, nothing else", "", "", "TZif2...", "", ""},
		{"symlink outside zoneinfo", "/opt/tz/local", "", "", "", ""},
		{"symlink outside zoneinfo, /etc/timezone", "/opt/tz/local", "", "", " UTC \n", "UTC"},
		{"symlink to zoneinfo itself", "/usr/share/zoneinfo/", "", "", "", ""},
		{"missing", "", "", "", "", ""},
	} {
		env := source.Env{FS: tzRoot(t, c.link, c.linkFile, c.regular, c.timezone)}
		if got := zone(env); got != c.want {
			t.Errorf("%s: zone = %q, want %q", c.name, got, c.want)
		}
	}
}

func timexStatus(status int32, err error) func() (int32, error) {
	return func() (int32, error) { return status, err }
}

func TestClockSynced(t *testing.T) {
	for _, c := range []struct {
		name       string
		timex      func() (int32, error)
		synced, ok bool
	}{
		{"unsync bit clear", timexStatus(unix.STA_PLL|unix.STA_NANO, nil), true, true},
		{"unsync bit set", timexStatus(unix.STA_PLL|unix.STA_UNSYNC, nil), false, true},
		{"only unsync", timexStatus(unix.STA_UNSYNC, nil), false, true},
		{"syscall error", timexStatus(0, errors.New("EPERM")), false, false},
		{"no timex", nil, false, false},
	} {
		synced, ok := kernelSynced(source.Env{Timex: c.timex})
		if synced != c.synced || ok != c.ok {
			t.Errorf("%s: synced %v ok %v, want %v %v", c.name, synced, ok, c.synced, c.ok)
		}
	}
}

// unitBus arma el systemd1 falso: load/active de ListUnitsByNames ("" = sin
// respuesta) y el estado del archivo de la unidad ("" = error NoSuchUnit).
func unitBus(load, active, fileState string) *source.FakeBus {
	m := map[string]json.RawMessage{}
	if load != "" {
		m[unitsKey] = json.RawMessage(`[[["systemd-timesyncd.service","Network Time Synchronization","` + load + `","` + active +
			`","dead","","/org/freedesktop/systemd1/unit/systemd_2dtimesyncd_2eservice",0,"","/"]]]`)
	}
	if fileState != "" {
		m[fileStateKey] = json.RawMessage(`["` + fileState + `"]`)
	}
	return &source.FakeBus{System: m}
}

func TestClockTimesyncd(t *testing.T) {
	for _, c := range []struct {
		name        string
		bus         *source.FakeBus
		canNTP, ntp bool
	}{
		{"systemd1 unavailable", &source.FakeBus{}, false, false},
		{"not found", unitBus("not-found", "inactive", ""), false, false},
		{"masked", unitBus("masked", "inactive", "masked"), false, false},
		{"disabled", unitBus("loaded", "inactive", "disabled"), true, false},
		{"enabled, not running", unitBus("loaded", "inactive", "enabled"), true, true},
		{"enabled-runtime", unitBus("loaded", "inactive", "enabled-runtime"), true, true},
		{"active, disabled", unitBus("loaded", "active", "disabled"), true, true},
		{"activating", unitBus("loaded", "activating", "disabled"), true, true},
		{"enabled and active", unitBus("loaded", "active", "enabled"), true, true},
		{"odd body", &source.FakeBus{System: map[string]json.RawMessage{unitsKey: json.RawMessage(`["x"]`), fileStateKey: json.RawMessage(`[1]`)}}, false, false},
	} {
		env := source.Env{Bus: c.bus}
		canNTP, ntp := timesyncd(env, ListUnits(env))
		if canNTP != c.canNTP || ntp != c.ntp {
			t.Errorf("%s: canNTP %v ntp %v, want %v %v", c.name, canNTP, ntp, c.canNTP, c.ntp)
		}
	}
}

// Sin ninguna fuente útil: nil, y nada en unavailable. Con alguna, lo que haya.
func TestClockMissing(t *testing.T) {
	r := &collect.Report{Lang: "en"}
	s := &model.System{}
	Slow(source.Fixture(t, "laptop"), s, r)
	if s.Time != nil {
		t.Fatalf("time = %+v", s.Time)
	}
	for _, u := range r.List {
		if strings.HasPrefix(u.What, "system.time") {
			t.Fatalf("unexpected unavailable %+v", u)
		}
	}
	empty := source.Env{FS: tzRoot(t, "", "", "", ""), Bus: &source.FakeBus{}}
	if tm := clock(empty, ListUnits(empty)); tm != nil {
		t.Errorf("nothing: %+v", tm)
	}
	empty.Timex = timexStatus(0, errors.New("EPERM"))
	if tm := clock(empty, ListUnits(empty)); tm != nil {
		t.Errorf("timex error: %+v", tm)
	}
	for _, c := range []struct {
		name string
		env  source.Env
		want model.TimeSync
	}{
		{"only zone", source.Env{FS: tzRoot(t, "/usr/share/zoneinfo/UTC", "", "", ""), Bus: &source.FakeBus{}}, model.TimeSync{Zone: "UTC"}},
		{"only kernel", source.Env{FS: tzRoot(t, "", "", "", ""), Bus: &source.FakeBus{}, Timex: timexStatus(0, nil)}, model.TimeSync{Synced: true}},
		{"only timesyncd", source.Env{FS: tzRoot(t, "", "", "", ""), Bus: unitBus("loaded", "inactive", "disabled")}, model.TimeSync{CanNTP: true}},
	} {
		if tm := clock(c.env, ListUnits(c.env)); tm == nil || *tm != c.want {
			t.Errorf("%s: %+v", c.name, tm)
		}
	}
}

// El reloj no activa timedated: el fixture ya no responde por timedate1.
func TestClockNoTimedate1(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(source.FixturesDir(t), "threadripper", "dbus.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "timedate1") {
		t.Error("threadripper fixture still has timedate1")
	}
}
