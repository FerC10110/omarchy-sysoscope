package actlog

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

func TestWrite(t *testing.T) {
	env := source.Env{State: filepath.Join(t.TempDir(), "state"),
		Now: func() time.Time { return time.Date(2026, 9, 26, 18, 0, 0, 0, time.FixedZone("ART", -3*3600)) }}
	Write(env, "power.profile", "", "performance", "Power profile set to performance", nil)
	Write(env, "app.freeze", "app:app-x.scope", "", "", errors.New("X keeps the session running"))
	b, err := os.ReadFile(filepath.Join(env.State, "actions.log"))
	if err != nil {
		t.Fatal(err)
	}
	want := "2026-09-26T21:00:00Z power.profile \"\" \"performance\" ok: Power profile set to performance\n" +
		"2026-09-26T21:00:00Z app.freeze \"app:app-x.scope\" \"\" error: X keeps the session running\n"
	if string(b) != want {
		t.Fatalf("log:\n%s", b)
	}
}

// Lo que identifica no se anota: de Wi-Fi, Bluetooth y audio, ni la fila
// (la red, la dirección, el nombre de pactl) ni el texto; de las ventanas,
// el texto (lleva el título o la ruta de la captura). El dato queda solo
// en las de sección (y el volumen), que no dicen nada del usuario.
func TestWritePrivate(t *testing.T) {
	env := source.Env{State: filepath.Join(t.TempDir(), "state"),
		Now: func() time.Time { return time.Date(2026, 9, 26, 18, 0, 0, 0, time.UTC) }}
	Write(env, "wifi.forget", "wlp3s0:SSID-SENTINEL", "", "SSID-SENTINEL was forgotten", nil)
	Write(env, "wifi.connect", "wlp3s0:hidden:aa:bb:cc:dd:ee:ff", "x", "", errors.New("no network SSID-SENTINEL"))
	Write(env, "wifi.band", "", "5", "Changing the band to 5…", nil)
	Write(env, "bt.connect", "aa:bb:cc:dd:ee:01", "", "Connected to BT-SENTINEL", nil)
	Write(env, "bt.power", "", "", "Turning Bluetooth on…", nil)
	Write(env, "audio.volume", "bluez_output.AA_BB_CC_DD_EE_02.1", "40", "Volume 40% on BT-SENTINEL", nil)
	Write(env, "window.float", "0xa1", "", "Toggle floating: TITLE-SENTINEL", nil)
	Write(env, "window.screenshot", "0xa1", "", "saved to /home/SENTINEL/x.png", nil)
	b, err := os.ReadFile(filepath.Join(env.State, "actions.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range []string{"SENTINEL", "aa:bb", "AA_BB"} {
		if strings.Contains(string(b), x) {
			t.Errorf("actions.log has %q", x)
		}
	}
	want := "2026-09-26T18:00:00Z wifi.forget \"\" \"\" ok\n" +
		"2026-09-26T18:00:00Z wifi.connect \"\" \"\" error\n" +
		"2026-09-26T18:00:00Z wifi.band \"\" \"5\" ok\n" +
		"2026-09-26T18:00:00Z bt.connect \"\" \"\" ok\n" +
		"2026-09-26T18:00:00Z bt.power \"\" \"\" ok\n" +
		"2026-09-26T18:00:00Z audio.volume \"\" \"40\" ok\n" +
		"2026-09-26T18:00:00Z window.float \"0xa1\" \"\" ok\n" +
		"2026-09-26T18:00:00Z window.screenshot \"0xa1\" \"\" ok\n"
	if string(b) != want {
		t.Fatalf("log:\n%s", b)
	}
}
