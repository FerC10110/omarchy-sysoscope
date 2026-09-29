package wifi

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

type spawn struct {
	name string
	args []string
}

// stubSpawn reemplaza todo lo que lanzaría un proceso: acá nada corre de
// verdad. Devuelve lo lanzado desacoplado y lo corrido en la terminal.
func stubSpawn(t *testing.T, tty bool) (launched, attached *[]spawn) {
	t.Helper()
	var l, a []spawn
	oldL, oldI, oldT, oldE, oldP := launch, interactive, isTTY, executable, lookPath
	launch = func(name string, args ...string) error { l = append(l, spawn{name, args}); return nil }
	interactive = func(name string, args ...string) error { a = append(a, spawn{name, args}); return nil }
	isTTY = func() bool { return tty }
	executable = func() (string, error) { return "/opt/sysoscope", nil }
	lookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	t.Cleanup(func() { launch, interactive, isTTY, executable, lookPath = oldL, oldI, oldT, oldE, oldP })
	return &l, &a
}

// acted son las órdenes que cambian algo (todo lo que no es una lectura).
func acted(env source.Env) []string {
	var out []string
	for _, c := range runs(env).Calls {
		if !strings.HasPrefix(c, "nmcli_-t_") && !strings.HasPrefix(c, "nmcli_-e_no_-g_") {
			out = append(out, c)
		}
	}
	return out
}

const cafeUUID = "8c9d0e1f-0000-4000-8000-000000000003"

func TestKnown(t *testing.T) {
	for _, id := range []string{"wifi.radio", "wifi.rescan", "wifi.band", "wifi.connect", "wifi.connect_new", "wifi.connect_hidden", "wifi.disconnect", "wifi.forget"} {
		if !Known(id) {
			t.Errorf("%s unknown", id)
		}
	}
	if Known("wifi.nope") || Known("window.float") || !IsSection("wifi.band") || IsSection("wifi.connect") {
		t.Fatal("Known/IsSection")
	}
}

func TestRadioOff(t *testing.T) {
	env := tempWifi(t)
	out, err := Run(env, "wifi.radio", "", "")
	if err != nil || out != "Wi-Fi turned off" || !slices.Equal(acted(env), []string{"nmcli_radio_wifi_off"}) {
		t.Fatalf("%q, %v, acted %q", out, err, acted(env))
	}
}

// Al prender, si rfkill sigue bloqueando la placa, se desbloquea.
func TestRadioOnUnblocksRfkill(t *testing.T) {
	env := tempWifi(t)
	put(t, env, "cmd/nmcli_-t_radio_wifi.txt", "disabled\n")
	put(t, env, "root/sys/class/rfkill/rfkill1/soft", "1\n")
	out, err := Run(env, "wifi.radio", "", "")
	if err != nil || out != "Wi-Fi turned on" || !slices.Equal(acted(env), []string{"nmcli_radio_wifi_on", "rfkill_unblock_wlan"}) {
		t.Fatalf("%q, %v, acted %q", out, err, acted(env))
	}
	env = tempWifi(t)
	put(t, env, "cmd/nmcli_-t_radio_wifi.txt", "disabled\n")
	if _, err := Run(env, "wifi.radio", "", ""); err != nil || !slices.Equal(acted(env), []string{"nmcli_radio_wifi_on"}) {
		t.Fatalf("without an rfkill block: %v, acted %q", err, acted(env))
	}
}

func TestRadioHard(t *testing.T) {
	env := tempWifi(t)
	put(t, env, "cmd/nmcli_-t_radio_wifi.txt", "disabled\n")
	put(t, env, "root/sys/class/rfkill/rfkill1/hard", "1\n")
	if _, err := Run(env, "wifi.radio", "", ""); err == nil || err.Error() != "the Wi-Fi is switched off by hardware (a key or a switch)" || len(acted(env)) != 0 {
		t.Fatalf("%v, acted %q", err, acted(env))
	}
}

func TestRescan(t *testing.T) {
	env := tempWifi(t)
	out, err := Run(env, "wifi.rescan", "", "")
	if err != nil || out != "Searching for networks on wlp3s0" || !slices.Equal(acted(env), []string{"nmcli_device_wifi_rescan_ifname_wlp3s0"}) {
		t.Fatalf("%q, %v, acted %q", out, err, acted(env))
	}
}

func TestBand(t *testing.T) {
	launched, _ := stubSpawn(t, false)
	env := tempWifi(t)
	out, err := Run(env, "wifi.band", "", "5")
	want := spawn{"/opt/sysoscope", []string{"wifi-wait", "wifi.band", "", "5", "--", "omarchy-network-band", "5"}}
	if err != nil || out != "Changing the band to 5…" || len(*launched) != 1 || (*launched)[0].name != want.name || !slices.Equal((*launched)[0].args, want.args) {
		t.Fatalf("%q, %v, launched %+v", out, err, *launched)
	}
	if _, err := Run(env, "wifi.band", "", "7"); err == nil || err.Error() != "one of: auto, 2.4, 5, 6" || len(*launched) != 1 {
		t.Fatalf("bad band: %v", err)
	}
	put(t, env, "cmd/nmcli_-t_-f_DEVICE,STATE,CONNECTION_device.txt", "wlp3s0:disconnected:\nwlp4s0:unavailable:\n")
	put(t, env, "cmd/nmcli_-t_-f_IN-USE,SSID,SIGNAL,FREQ,SECURITY,BSSID_device_wifi_list_ifname_wlp3s0_--rescan_no.txt", " :Plaza libre:30:2462 MHz::AA\\:BB\n")
	if _, err := Run(env, "wifi.band", "", "5"); err == nil || err.Error() != "no Wi-Fi network is connected" || len(*launched) != 1 {
		t.Fatalf("not connected: %v", err)
	}
}

func TestConnectSavedAndOpen(t *testing.T) {
	launched, attached := stubSpawn(t, true)
	env := tempWifi(t)
	out, err := Run(env, "wifi.connect", "wlp3s0:Café: 2º piso", "")
	if err != nil || out != `Connecting to "Café: 2º piso"…` {
		t.Fatalf("saved: %q, %v", out, err)
	}
	if _, err := Run(env, "wifi.connect", "wlp3s0:Plaza libre", ""); err != nil {
		t.Fatalf("open: %v", err)
	}
	want := [][]string{
		{"wifi-wait", "wifi.connect", "wlp3s0:Café: 2º piso", "", "--", "nmcli", "--wait", "45", "connection", "up", "uuid", cafeUUID, "ifname", "wlp3s0"},
		{"wifi-wait", "wifi.connect", "wlp3s0:Plaza libre", "", "--", "nmcli", "--wait", "45", "device", "wifi", "connect", "Plaza libre", "ifname", "wlp3s0"},
	}
	if len(*launched) != 2 || len(*attached) != 0 || len(acted(env)) != 0 {
		t.Fatalf("launched %+v, attached %+v, acted %q", *launched, *attached, acted(env))
	}
	for i, l := range *launched {
		if l.name != "/opt/sysoscope" || !slices.Equal(l.args, want[i]) {
			t.Errorf("launched[%d] = %+v", i, l)
		}
	}
	if _, err := Run(env, "wifi.connect", "wlp3s0:Casa", ""); err == nil || err.Error() != "already connected to Casa" {
		t.Fatalf("in use: %v", err)
	}
}

// Una red nueva con clave: con terminal, nmtui ahí mismo; sin terminal (el
// panel), en una ventana con omarchy-launch-tui. La clave nunca pasa por acá.
func TestConnectNew(t *testing.T) {
	launched, attached := stubSpawn(t, true)
	env := tempWifi(t)
	if out, err := Run(env, "wifi.connect_new", "wlp3s0:Vecino 🚀", ""); err != nil || out != "Opened nmtui for Vecino 🚀" {
		t.Fatalf("%q, %v", out, err)
	}
	if len(*launched) != 0 || len(*attached) != 1 || (*attached)[0].name != "nmtui" || !slices.Equal((*attached)[0].args, []string{"connect", "Vecino 🚀"}) {
		t.Fatalf("with a terminal: launched %+v, attached %+v", *launched, *attached)
	}
	launched, attached = stubSpawn(t, false)
	if _, err := Run(env, "wifi.connect_new", "wlp3s0:Vecino 🚀", ""); err != nil {
		t.Fatal(err)
	}
	if len(*attached) != 0 || len(*launched) != 1 || (*launched)[0].name != "omarchy-launch-tui" ||
		!slices.Equal((*launched)[0].args, []string{"nmtui", "connect", "Vecino 🚀"}) {
		t.Fatalf("without a terminal: launched %+v, attached %+v", *launched, *attached)
	}
}

// lazylazy manda wifi.connect también para una red nueva con clave (no
// tiene hide): el colector abre nmtui igual.
func TestConnectLockedFromLazylazy(t *testing.T) {
	_, attached := stubSpawn(t, true)
	env := tempWifi(t)
	if _, err := Run(env, "wifi.connect", "wlp3s0:Vecino 🚀", ""); err != nil || len(*attached) != 1 || (*attached)[0].name != "nmtui" {
		t.Fatalf("%v, attached %+v", err, *attached)
	}
}

func TestConnectNewHostileSSID(t *testing.T) {
	launched, _ := stubSpawn(t, false)
	env := tempWifi(t)
	if _, err := Run(env, "wifi.connect_new", `wlp3s0:$(reboot); "x"`, ""); err != nil {
		t.Fatal(err)
	}
	l := (*launched)[0]
	if l.name != "omarchy-launch-tui" || !slices.Equal(l.args, []string{"nmtui", "connect", `$(reboot); "x"`}) {
		t.Fatalf("launched %+v", l)
	}
}

// La red o la placa cambiaron desde la muestra: error legible y ninguna orden.
func TestActionTargetGone(t *testing.T) {
	launched, attached := stubSpawn(t, false)
	env := tempWifi(t)
	for tg, want := range map[string]string{
		"wlp3s0:Nadie":   "the network Nadie is no longer in range",
		"wlp4s0:Casa":    "the Wi-Fi adapter wlp4s0 is gone or off",
		"wlp9s0:Casa":    "the Wi-Fi adapter wlp9s0 is gone or off",
		"sin-dos-puntos": `unknown network "sin-dos-puntos"`,
	} {
		for _, id := range []string{"wifi.connect", "wifi.connect_new", "wifi.disconnect", "wifi.forget"} {
			if _, err := Run(env, id, tg, ""); err == nil || err.Error() != want {
				t.Errorf("%s %s: %v, want %q", id, tg, err, want)
			}
		}
	}
	if len(acted(env)) != 0 || len(*launched) != 0 || len(*attached) != 0 {
		t.Fatalf("acted %q, launched %+v, attached %+v", acted(env), *launched, *attached)
	}
}

func TestDisconnectAndForget(t *testing.T) {
	env := tempWifi(t)
	if out, err := Run(env, "wifi.disconnect", "wlp3s0:Casa", ""); err != nil || out != "Disconnected from Casa" {
		t.Fatalf("disconnect: %q, %v", out, err)
	}
	if out, err := Run(env, "wifi.forget", "wlp3s0:Café: 2º piso", ""); err != nil || out != "Café: 2º piso was forgotten" {
		t.Fatalf("forget: %q, %v", out, err)
	}
	want := []string{"nmcli_device_disconnect_wlp3s0", "nmcli_connection_delete_uuid_" + cafeUUID}
	if !slices.Equal(acted(env), want) {
		t.Fatalf("acted %q", acted(env))
	}
}

func TestDisconnectAndForgetRefuse(t *testing.T) {
	env := tempWifi(t)
	if _, err := Run(env, "wifi.disconnect", "wlp3s0:Café: 2º piso", ""); err == nil || err.Error() != "this network is not connected" {
		t.Fatalf("disconnect: %v", err)
	}
	if _, err := Run(env, "wifi.forget", "wlp3s0:Plaza libre", ""); err == nil || err.Error() != "this network is not saved" {
		t.Fatalf("forget: %v", err)
	}
	if len(acted(env)) != 0 {
		t.Fatalf("acted %q", acted(env))
	}
}

// El error de nmcli (su primera línea) va al resultado.
func TestActionNmcliError(t *testing.T) {
	env := tempWifi(t)
	put(t, env, "cmd/nmcli_device_disconnect_wlp3s0.txt", "Error: Device 'wlp3s0' not active.\n")
	put(t, env, "cmd/nmcli_device_disconnect_wlp3s0.exit", "6\n")
	if _, err := Run(env, "wifi.disconnect", "wlp3s0:Casa", ""); err == nil || err.Error() != "Device 'wlp3s0' not active." {
		t.Fatalf("%v", err)
	}
}

func TestActionsWithoutNetworkManager(t *testing.T) {
	env := tempWifi(t)
	env.Run = &source.FakeRunner{Dir: t.TempDir()}
	for _, id := range []string{"wifi.radio", "wifi.rescan", "wifi.disconnect"} {
		if _, err := Run(env, id, "wlp3s0:Casa", ""); err == nil || err.Error() != "Wi-Fi not available: NetworkManager is needed" {
			t.Errorf("%s: %v", id, err)
		}
	}
}

func TestWifiActionsLogAndSpanish(t *testing.T) {
	env := tempWifi(t)
	env.Lang = "es"
	if out, _ := Run(env, "wifi.disconnect", "wlp3s0:Casa", ""); out != "Desconectado de Casa" {
		t.Fatalf("es = %q", out)
	}
	env.Lang = "en"
	Run(env, "wifi.forget", "wlp3s0:Plaza libre", "")
	b, err := os.ReadFile(filepath.Join(env.State, "actions.log"))
	if err != nil {
		t.Fatal(err)
	}
	// Sin la red ni el texto (la nombra): ver actlog.
	want := "2026-09-23T12:00:00Z wifi.disconnect \"\" \"\" ok\n" +
		"2026-09-23T12:00:00Z wifi.forget \"\" \"\" error\n"
	if string(b) != want {
		t.Fatalf("log:\n%s", b)
	}
}

// wifi-wait corre la orden hasta el final y anota el resultado.
func TestWait(t *testing.T) {
	env := tempWifi(t)
	argv := []string{"nmcli", "--wait", "45", "connection", "up", "uuid", cafeUUID, "ifname", "wlp3s0"}
	if err := Wait(env, "wifi.connect", "wlp3s0:Café: 2º piso", "", argv); err != nil {
		t.Fatal(err)
	}
	key := "cmd/" + source.Key(argv[0], argv[1:]...)
	put(t, env, key+".txt", "Error: Connection activation failed: Secrets were required, but not provided.\n")
	put(t, env, key+".exit", "4\n")
	if err := Wait(env, "wifi.connect", "wlp3s0:Café: 2º piso", "", argv); err == nil {
		t.Fatal("a failed connection returned no error")
	}
	if err := Wait(env, "wifi.connect", "wlp3s0:Casa", "", []string{"rm", "-rf", "/"}); err == nil || err.Error() != "wifi-wait only runs nmcli or omarchy-network-band" {
		t.Fatalf("other command: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(env.State, "actions.log"))
	want := "2026-09-23T12:00:00Z wifi.connect \"\" \"\" ok\n" +
		"2026-09-23T12:00:00Z wifi.connect \"\" \"\" error\n" +
		"2026-09-23T12:00:00Z wifi.connect \"\" \"\" error\n"
	if string(b) != want {
		t.Fatalf("log:\n%s", b)
	}
	if slices.Contains(runs(env).Calls, "rm_-rf_-") {
		t.Fatal("wifi-wait ran something other than nmcli")
	}
}

// El lanzador real: `true` es lo único que un test lanza de verdad.
func TestLaunchDetached(t *testing.T) {
	if err := LaunchDetached("true"); err != nil {
		t.Fatal(err)
	}
	if err := LaunchDetached("sysoscope-no-such-binary"); err == nil {
		t.Fatal("a missing binary did not fail")
	}
}

const (
	hiddenBSSID = "AA:BB:CC:00:00:05" // la oculta del fixture wifi, en wlp3s0
	hiddenRow   = "wlp3s0:hidden:" + hiddenBSSID
	listKey     = "cmd/nmcli_-t_-f_IN-USE,SSID,SIGNAL,FREQ,SECURITY,BSSID_device_wifi_list_ifname_wlp3s0_--rescan_no.txt"
)

// Una oculta con terminal (lazylazy en pantalla completa): `sysoscope
// wifi-hidden` ahí mismo, que pide el nombre; nmcli pide la clave.
func TestConnectHiddenWithTerminal(t *testing.T) {
	launched, attached := stubSpawn(t, true)
	env := tempWifi(t)
	out, err := Run(env, "wifi.connect_hidden", hiddenRow, "")
	if err != nil || out != "Connected to the hidden network "+hiddenBSSID {
		t.Fatalf("%q, %v", out, err)
	}
	want := spawn{"/opt/sysoscope", []string{"wifi-hidden", "wlp3s0", hiddenBSSID}}
	if len(*launched) != 0 || len(*attached) != 1 || (*attached)[0].name != want.name || !slices.Equal((*attached)[0].args, want.args) || len(acted(env)) != 0 {
		t.Fatalf("launched %+v, attached %+v, acted %q", *launched, *attached, acted(env))
	}
}

// wifi-hidden anota su propio resultado: con terminal, el padre no agrega
// otra línea (una por intento), salvo si wifi-hidden no llegó a intentar
// (uso incorrecto, salida 2). Cancelado (salida 3) no es "no se conectó".
func TestConnectHiddenWithTerminalExitCodes(t *testing.T) {
	for _, c := range []struct {
		code      int
		want, log string
	}{
		{0, "", ""},
		{1, "the hidden network " + hiddenBSSID + " was not connected", ""},
		{3, "cancelled", ""},
		{2, "wifi-hidden failed (exit 2)", `wifi.connect_hidden "" "" error`},
	} {
		stubSpawn(t, true)
		exitErr := exec.Command("sh", "-c", fmt.Sprintf("exit %d", c.code)).Run()
		interactive = func(string, ...string) error { return exitErr }
		env := tempWifi(t)
		out, err := Run(env, "wifi.connect_hidden", hiddenRow, "")
		switch {
		case c.code == 0 && (err != nil || out != "Connected to the hidden network "+hiddenBSSID):
			t.Errorf("exit 0: %q, %v", out, err)
		case c.code != 0 && (err == nil || err.Error() != c.want):
			t.Errorf("exit %d: %v, want %q", c.code, err, c.want)
		}
		b, _ := os.ReadFile(filepath.Join(env.State, "actions.log"))
		if c.log == "" && len(b) != 0 || c.log != "" && !strings.HasSuffix(string(b), c.log+"\n") {
			t.Errorf("exit %d: log:\n%s", c.code, b)
		}
	}
}

// Sin terminal (el panel): una ventana con omarchy-launch-tui, con --hold
// (espera Enter antes de cerrarse) y el idioma.
func TestConnectHiddenWithoutTerminal(t *testing.T) {
	launched, attached := stubSpawn(t, false)
	env := tempWifi(t)
	env.Lang = "es"
	out, err := Run(env, "wifi.connect_hidden", hiddenRow, "")
	if err != nil || out != "Se abrió una ventana para conectar a la red oculta "+hiddenBSSID {
		t.Fatalf("%q, %v", out, err)
	}
	want := []string{"/opt/sysoscope", "wifi-hidden", "--hold", "--lang", "es", "wlp3s0", hiddenBSSID}
	if len(*attached) != 0 || len(*launched) != 1 || (*launched)[0].name != "omarchy-launch-tui" || !slices.Equal((*launched)[0].args, want) {
		t.Fatalf("launched %+v, attached %+v", *launched, *attached)
	}
}

// lazylazy manda wifi.connect también sobre una oculta (no tiene hide).
func TestConnectOnHiddenRow(t *testing.T) {
	_, attached := stubSpawn(t, true)
	env := tempWifi(t)
	if _, err := Run(env, "wifi.connect", hiddenRow, ""); err != nil || len(*attached) != 1 ||
		!slices.Equal((*attached)[0].args, []string{"wifi-hidden", "wlp3s0", hiddenBSSID}) {
		t.Fatalf("%v, attached %+v", err, *attached)
	}
}

// La oculta ya no está, está conectada, o la acción no es conectar: error
// legible y nada corre.
func TestConnectHiddenRefuses(t *testing.T) {
	launched, attached := stubSpawn(t, false)
	env := tempWifi(t)
	for _, c := range []struct{ id, tg, want string }{
		{"wifi.connect_hidden", "wlp3s0:hidden:AA:BB:CC:00:00:99", "the hidden network AA:BB:CC:00:00:99 is no longer in range"},
		{"wifi.connect", "wlp3s0:hidden:AA:BB:CC:00:00:99", "the hidden network AA:BB:CC:00:00:99 is no longer in range"},
		{"wifi.connect_hidden", "wlp4s0:hidden:" + hiddenBSSID, "the Wi-Fi adapter wlp4s0 is gone or off"},
		{"wifi.connect_hidden", "wlp3s0:Casa", "this network is not hidden"},
		{"wifi.disconnect", hiddenRow, "this network is not connected"},
		{"wifi.forget", hiddenRow, "a hidden network can only be connected or disconnected"},
		{"wifi.connect_new", hiddenRow, "a hidden network can only be connected or disconnected"},
	} {
		if _, err := Run(env, c.id, c.tg, ""); err == nil || err.Error() != c.want {
			t.Errorf("%s %s: %v, want %q", c.id, c.tg, err, c.want)
		}
	}
	put(t, env, listKey, "*::62:5500 MHz:WPA2:AA\\:BB\\:CC\\:00\\:00\\:05\n")
	if _, err := Run(env, "wifi.connect_hidden", hiddenRow, ""); err == nil || err.Error() != "already connected to (hidden network) "+hiddenBSSID {
		t.Errorf("in use: %v", err)
	}
	if len(acted(env)) != 0 || len(*launched) != 0 || len(*attached) != 0 {
		t.Fatalf("acted %q, launched %+v, attached %+v", acted(env), *launched, *attached)
	}
}

// Un SSID real que empieza con "hidden:" es una red con nombre, salvo que
// haya una oculta con exactamente ese BSSID: ahí gana la oculta (conectar a
// ella solo pide el nombre, no hace nada sin confirmar).
func TestResolveHiddenPrefixSSID(t *testing.T) {
	launched, _ := stubSpawn(t, false)
	env := tempWifi(t)
	put(t, env, listKey, " :hidden\\:AA\\:BB\\:CC\\:00\\:00\\:09:40:2412 MHz::AA\\:BB\\:CC\\:00\\:00\\:0A\n"+
		" :hidden\\:x:35:2412 MHz::AA\\:BB\\:CC\\:00\\:00\\:0B\n"+
		" :hidden\\:AA\\:BB\\:CC\\:00\\:00\\:05:30:2412 MHz::AA\\:BB\\:CC\\:00\\:00\\:0C\n"+
		" ::62:5500 MHz:WPA2:AA\\:BB\\:CC\\:00\\:00\\:05\n")
	for _, tg := range []string{"wlp3s0:hidden:AA:BB:CC:00:00:09", "wlp3s0:hidden:x"} {
		if _, err := Run(env, "wifi.connect", tg, ""); err != nil {
			t.Fatalf("%s: %v", tg, err)
		}
	}
	if _, err := Run(env, "wifi.connect_hidden", "wlp3s0:hidden:AA:BB:CC:00:00:09", ""); err == nil ||
		err.Error() != "this network is not hidden" {
		t.Fatalf("connect_hidden on a named network: %v", err)
	}
	if _, err := Run(env, "wifi.connect", hiddenRow, ""); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"wifi-wait", "wifi.connect", "wlp3s0:hidden:AA:BB:CC:00:00:09", "", "--", "nmcli", "--wait", "45", "device", "wifi", "connect", "hidden:AA:BB:CC:00:00:09", "ifname", "wlp3s0"},
		{"wifi-wait", "wifi.connect", "wlp3s0:hidden:x", "", "--", "nmcli", "--wait", "45", "device", "wifi", "connect", "hidden:x", "ifname", "wlp3s0"},
	}
	if len(*launched) != 3 || (*launched)[2].name != "omarchy-launch-tui" || (*launched)[2].args[1] != "wifi-hidden" {
		t.Fatalf("launched %+v", *launched)
	}
	for i, w := range want {
		if (*launched)[i].name != "/opt/sysoscope" || !slices.Equal((*launched)[i].args, w) {
			t.Errorf("launched[%d] = %+v", i, (*launched)[i])
		}
	}
}

// Una oculta conectada se desconecta como cualquiera: solo hace falta la placa.
func TestDisconnectHidden(t *testing.T) {
	env := tempWifi(t)
	put(t, env, listKey, "*::62:5500 MHz:WPA2:AA\\:BB\\:CC\\:00\\:00\\:05\n")
	out, err := Run(env, "wifi.disconnect", hiddenRow, "")
	if err != nil || out != "Disconnected from (hidden network) "+hiddenBSSID || !slices.Equal(acted(env), []string{"nmcli_device_disconnect_wlp3s0"}) {
		t.Fatalf("%q, %v, acted %q", out, err, acted(env))
	}
}
