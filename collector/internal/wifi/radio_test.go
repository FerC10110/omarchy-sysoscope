package wifi

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// fakeSys arma un /sys falso: ruta → contenido (sin el "\n" final).
func fakeSys(t *testing.T, files map[string]string) source.FS {
	t.Helper()
	dir := t.TempDir()
	for p, v := range files {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(v+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return source.FS{Root: dir}
}

// rfkillSys es wlp3s0 (phy0) con su rfkill y un Bluetooth bloqueado, que no cuenta.
func rfkillSys(t *testing.T, soft, hard string) source.FS {
	return fakeSys(t, map[string]string{
		"sys/class/net/wlp3s0/wireless/.keep": "",
		"sys/class/net/wlp3s0/phy80211/name":  "phy0",
		"sys/class/rfkill/rfkill0/type":       "bluetooth",
		"sys/class/rfkill/rfkill0/name":       "hci0",
		"sys/class/rfkill/rfkill0/soft":       "1",
		"sys/class/rfkill/rfkill0/hard":       "0",
		"sys/class/rfkill/rfkill1/type":       "wlan",
		"sys/class/rfkill/rfkill1/name":       "phy0",
		"sys/class/rfkill/rfkill1/soft":       soft,
		"sys/class/rfkill/rfkill1/hard":       hard,
		"sys/class/net/enp5s0/address":        "02:00:00:00:00:05",
	})
}

func TestRfkill(t *testing.T) {
	for _, c := range []struct{ soft, hard, want string }{{"0", "0", "on"}, {"1", "0", "soft"}, {"1", "1", "hard"}, {"0", "1", "hard"}} {
		if got := Rfkill(rfkillSys(t, c.soft, c.hard), "wlp3s0"); got != c.want {
			t.Errorf("soft=%s hard=%s: %q, want %q", c.soft, c.hard, got, c.want)
		}
	}
	noRfkill := fakeSys(t, map[string]string{"sys/class/net/wlp3s0/phy80211/name": "phy0"})
	if got := Rfkill(noRfkill, "wlp3s0"); got != "" {
		t.Errorf("without rfkill = %q", got)
	}
	if got := Rfkill(rfkillSys(t, "1", "0"), "enp5s0"); got != "" {
		t.Errorf("without phy80211 = %q", got)
	}
}

func TestRadio(t *testing.T) {
	noRfkill := fakeSys(t, map[string]string{"sys/class/net/wlp3s0/phy80211/name": "phy0"})
	for _, c := range []struct {
		fs   source.FS
		nmOn bool
		want string
	}{
		{rfkillSys(t, "0", "0"), true, "on"},
		{rfkillSys(t, "0", "0"), false, "soft"},
		{rfkillSys(t, "1", "0"), true, "soft"},
		{rfkillSys(t, "0", "1"), true, "hard"},
		{noRfkill, true, "on"},
		{noRfkill, false, "soft"},
	} {
		if got := Radio(c.fs, "wlp3s0", c.nmOn); got != c.want {
			t.Errorf("Radio(nmOn=%v) = %q, want %q", c.nmOn, got, c.want)
		}
	}
}

func TestIfaces(t *testing.T) {
	if got := Ifaces(rfkillSys(t, "0", "0")); !slices.Equal(got, []string{"wlp3s0"}) {
		t.Fatalf("Ifaces = %q", got)
	}
	if got := Ifaces(source.FS{Root: t.TempDir()}); len(got) != 0 {
		t.Fatalf("Ifaces without /sys/class/net = %q", got)
	}
}
