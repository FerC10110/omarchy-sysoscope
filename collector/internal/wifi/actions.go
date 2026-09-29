package wifi

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/actlog"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

var (
	sectionIDs = []string{"wifi.radio", "wifi.rescan", "wifi.band"}
	rowIDs     = []string{"wifi.connect", "wifi.connect_new", "wifi.connect_hidden", "wifi.disconnect", "wifi.forget"}
)

// Known dice si id es una acción de Wi-Fi.
func Known(id string) bool { return slices.Contains(sectionIDs, id) || slices.Contains(rowIDs, id) }

// IsSection dice si la acción es de toda la sección (no lleva fila).
func IsSection(id string) bool { return slices.Contains(sectionIDs, id) }

const (
	// actionTimeout: lazylazy corta una acción en segundo plano a los 5 s.
	actionTimeout = 4 * time.Second
	// waitTimeout es lo máximo que espera wifi-wait: nmcli --wait 45, y la
	// banda reconecta y, si falla, vuelve a la anterior.
	waitTimeout = 3 * time.Minute
)

// Lo que lanza procesos; los tests lo reemplazan y nada corre de verdad.
var (
	launch      = LaunchDetached
	interactive = runInteractive
	isTTY       = stdinIsTTY
	executable  = os.Executable
	lookPath    = exec.LookPath
)

// Run corre la acción de Wi-Fi id sobre target ("<placa>:<ssid>",
// "<placa>:hidden:<BSSID>" en una oculta, o "" en las de sección) con input
// (la banda) y la anota en actions.log, salvo que ya la haya anotado
// wifi-hidden corriendo en esta terminal (una línea por intento). Devuelve
// el texto para la barra de estado.
func Run(env source.Env, id, target, input string) (string, error) {
	out, err := run(env, id, target, input)
	var done *childLogged
	if errors.As(err, &done) {
		return done.out, done.err
	}
	actlog.Write(env, id, target, input, out, err)
	return out, err
}

// childLogged es el resultado de un wifi-hidden que ya lo anotó él mismo.
type childLogged struct {
	out string
	err error
}

func (c *childLogged) Error() string { return "already logged" }

// Salidas de `sysoscope wifi-hidden`: 1 no se conectó, 2 uso incorrecto (no
// llegó a intentar ni a anotar), 3 cancelado.
const (
	HiddenExitFailed    = 1
	HiddenExitUsage     = 2
	HiddenExitCancelled = 3
)

func run(env source.Env, id, target, input string) (string, error) {
	l := env.Lang
	switch id {
	case "wifi.radio":
		return radio(env)
	case "wifi.rescan":
		return rescan(env, target)
	case "wifi.band":
		return band(env, input)
	}
	if !Known(id) {
		return "", errors.New(i18n.F(l, "unknown action %q", id))
	}
	t, err := resolve(env, target)
	if err != nil {
		return "", err
	}
	name := Printable(t.net.SSID)
	if t.net.Hidden {
		// Una oculta se conecta (wifi.connect también, porque lazylazy, sin
		// hide, manda esa) o, si está conectada, se desconecta: eso solo usa
		// la placa. No se olvida: no se sabe cuál es su conexión guardada.
		switch id {
		case "wifi.connect", "wifi.connect_hidden":
			return connectHidden(env, t)
		case "wifi.disconnect":
			name = i18n.T(l, "(hidden network)") + " " + t.net.BSSID
		default:
			return "", errors.New(i18n.T(l, "a hidden network can only be connected or disconnected"))
		}
	} else if id == "wifi.connect_hidden" {
		return "", errors.New(i18n.T(l, "this network is not hidden"))
	}
	switch id {
	case "wifi.disconnect":
		if !t.net.InUse {
			return "", errors.New(i18n.T(l, "this network is not connected"))
		}
		if err := command(env, "nmcli", "device", "disconnect", t.iface); err != nil {
			return "", err
		}
		return i18n.F(l, "Disconnected from %s", name), nil
	case "wifi.forget":
		if !t.net.Saved {
			return "", errors.New(i18n.T(l, "this network is not saved"))
		}
		if err := command(env, "nmcli", "connection", "delete", "uuid", t.net.UUID); err != nil {
			return "", err
		}
		return i18n.F(l, "%s was forgotten", name), nil
	}
	return connect(env, id, target, t)
}

// current lee el Wi-Fi ahora: la fila pudo cambiar desde la muestra.
func current(env source.Env) (*Scan, error) {
	var r Reader
	sc := r.Read(env, Ifaces(env.FS), env.Now())
	switch {
	case sc.Manager == "none":
		return nil, errors.New(i18n.T(env.Lang, "Wi-Fi not available: NetworkManager is needed"))
	case sc.Err != "":
		return nil, errors.New(sc.Err)
	}
	return sc, nil
}

type target struct {
	iface string
	net   model.WifiNet
}

// resolve busca la red de una fila en una lectura nueva. La fila es
// "<placa>:<ssid>" (se corta en el primer ":"; el SSID puede tenerlo) o, en
// una oculta, "<placa>:hidden:<BSSID>". Un SSID real puede escribirse igual
// que el id de una oculta: si el resto es "hidden:" más un BSSID y hay una
// oculta con exactamente ese BSSID, es ella; si no, se busca como SSID.
func resolve(env source.Env, tg string) (*target, error) {
	l := env.Lang
	iface, rest, ok := strings.Cut(tg, ":")
	if !ok || iface == "" || rest == "" {
		return nil, errors.New(i18n.F(l, "unknown network %q", tg))
	}
	sc, err := current(env)
	if err != nil {
		return nil, err
	}
	is, ok := sc.Ifaces[iface]
	if !ok || is.Radio != "on" {
		return nil, errors.New(i18n.F(l, "the Wi-Fi adapter %s is gone or off", iface))
	}
	bssid, hidden := strings.CutPrefix(rest, "hidden:")
	hidden = hidden && IsBSSID(bssid)
	if hidden {
		for _, n := range is.Networks {
			if n.Hidden && n.BSSID == bssid {
				return &target{iface: iface, net: n}, nil
			}
		}
	}
	for _, n := range is.Networks {
		if !n.Hidden && n.SSID == rest {
			return &target{iface: iface, net: n}, nil
		}
	}
	if hidden {
		return nil, errors.New(i18n.F(l, "the hidden network %s is no longer in range", bssid))
	}
	return nil, errors.New(i18n.F(l, "the network %s is no longer in range", Printable(rest)))
}

// connectHidden abre `sysoscope wifi-hidden`, que pide el nombre de la red y
// corre nmcli, que pide la clave en la terminal: ni el nombre ni la clave
// pasan por acá. Con terminal (lazylazy en pantalla completa) corre ahí
// mismo; sin terminal (el panel), en una ventana con omarchy-launch-tui, con
// --hold para que el resultado quede a la vista hasta Enter y con el idioma
// (la ventana no hereda el entorno de este proceso).
func connectHidden(env source.Env, t *target) (string, error) {
	l := env.Lang
	b := t.net.BSSID
	if t.net.InUse {
		return "", errors.New(i18n.F(l, "already connected to %s", i18n.T(l, "(hidden network)")+" "+b))
	}
	exe, err := executable()
	if err != nil {
		return "", err
	}
	if isTTY() {
		err := interactive(exe, "wifi-hidden", t.iface, b)
		var ee *exec.ExitError
		switch {
		case err == nil:
			return "", &childLogged{out: i18n.F(l, "Connected to the hidden network %s", b)}
		case !errors.As(err, &ee):
			return "", err // no arrancó: se anota acá
		case ee.ExitCode() == HiddenExitCancelled:
			return "", &childLogged{err: errors.New(i18n.T(l, "cancelled"))}
		case ee.ExitCode() == HiddenExitFailed:
			return "", &childLogged{err: errors.New(i18n.F(l, "the hidden network %s was not connected", b))}
		}
		return "", errors.New(i18n.F(l, "wifi-hidden failed (exit %d)", ee.ExitCode()))
	}
	if _, err := lookPath("omarchy-launch-tui"); err != nil {
		return "", errors.New(i18n.F(l, "requires %s", "omarchy-launch-tui"))
	}
	if err := launch("omarchy-launch-tui", exe, "wifi-hidden", "--hold", "--lang", l, t.iface, b); err != nil {
		return "", err
	}
	return i18n.F(l, "Opened a window to connect to the hidden network %s", b), nil
}

// connect: una guardada o abierta se conecta desacoplada (nmcli espera hasta
// 45 s); una nueva con clave se abre en nmtui, que la pide: nunca Sysoscope.
func connect(env source.Env, id, tg string, t *target) (string, error) {
	l := env.Lang
	n := t.net
	name := Printable(n.SSID)
	switch {
	case n.InUse:
		return "", errors.New(i18n.F(l, "already connected to %s", name))
	case n.Saved:
		return detached(env, id, tg, "", i18n.F(l, "Connecting to \"%s\"…", name),
			"nmcli", "--wait", "45", "connection", "up", "uuid", n.UUID, "ifname", t.iface)
	case n.Security == "":
		return detached(env, id, tg, "", i18n.F(l, "Connecting to \"%s\"…", name),
			"nmcli", "--wait", "45", "device", "wifi", "connect", n.SSID, "ifname", t.iface)
	}
	if isTTY() {
		if _, err := lookPath("nmtui"); err != nil {
			return "", errors.New(i18n.F(l, "requires %s", "nmtui"))
		}
		if err := interactive("nmtui", "connect", n.SSID); err != nil {
			return "", err
		}
		return i18n.F(l, "Opened nmtui for %s", name), nil
	}
	if _, err := lookPath("omarchy-launch-tui"); err != nil {
		return "", errors.New(i18n.F(l, "requires %s", "omarchy-launch-tui"))
	}
	if err := launch("omarchy-launch-tui", "nmtui", "connect", n.SSID); err != nil {
		return "", err
	}
	return i18n.F(l, "Opened nmtui for %s", name), nil
}

// radio apaga el Wi-Fi si alguna placa está encendida; si no, lo prende y,
// si rfkill sigue bloqueando alguna placa, la desbloquea.
func radio(env source.Env) (string, error) {
	l := env.Lang
	sc, err := current(env)
	if err != nil {
		return "", err
	}
	if len(sc.Ifaces) == 0 {
		return "", errors.New(i18n.T(l, "there is no Wi-Fi adapter"))
	}
	on, hard := false, true
	for _, is := range sc.Ifaces {
		on = on || is.Radio == "on"
		hard = hard && is.Radio == "hard"
	}
	if on {
		if err := command(env, "nmcli", "radio", "wifi", "off"); err != nil {
			return "", err
		}
		return i18n.T(l, "Wi-Fi turned off"), nil
	}
	if hard {
		return "", errors.New(i18n.T(l, "the Wi-Fi is switched off by hardware (a key or a switch)"))
	}
	if err := command(env, "nmcli", "radio", "wifi", "on"); err != nil {
		return "", err
	}
	for _, name := range Ifaces(env.FS) {
		if Rfkill(env.FS, name) == "soft" {
			if err := command(env, "rfkill", "unblock", "wlan"); err != nil {
				return "", err
			}
			break
		}
	}
	return i18n.T(l, "Wi-Fi turned on"), nil
}

// rescan pide un escaneo a cada placa encendida (o solo a la de tg).
func rescan(env source.Env, tg string) (string, error) {
	l := env.Lang
	sc, err := current(env)
	if err != nil {
		return "", err
	}
	var names []string
	for _, name := range Ifaces(env.FS) {
		if sc.Ifaces[name].Radio == "on" && (tg == "" || tg == name) {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "", errors.New(i18n.T(l, "the Wi-Fi is off"))
	}
	for _, name := range names {
		if err := command(env, "nmcli", "device", "wifi", "rescan", "ifname", name); err != nil {
			return "", err
		}
	}
	return i18n.F(l, "Searching for networks on %s", strings.Join(names, ", ")), nil
}

// band fija la banda de la conexión activa con omarchy-network-band, que
// reconecta: se lanza desacoplada.
func band(env source.Env, input string) (string, error) {
	l := env.Lang
	input = strings.TrimSpace(input)
	if !slices.Contains(Bands, input) {
		return "", errors.New(i18n.T(l, "one of: auto, 2.4, 5, 6"))
	}
	sc, err := current(env)
	if err != nil {
		return "", err
	}
	conn := false
	for _, is := range sc.Ifaces {
		for _, n := range is.Networks {
			conn = conn || n.InUse
		}
	}
	if !conn {
		return "", errors.New(i18n.T(l, "no Wi-Fi network is connected"))
	}
	if _, err := lookPath("omarchy-network-band"); err != nil {
		return "", errors.New(i18n.F(l, "requires %s", "omarchy-network-band"))
	}
	return detached(env, "wifi.band", "", input, i18n.F(l, "Changing the band to %s…", input), "omarchy-network-band", input)
}

// command corre una orden corta (hasta actionTimeout) y devuelve su error legible.
func command(env source.Env, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
	defer cancel()
	out, err := env.Run.Run(ctx, name, args...)
	if err != nil {
		return errors.New(Message(env.Lang, name, out, err))
	}
	return nil
}

// detached lanza `sysoscope wifi-wait ID FILA DATO -- ORDEN…` en su propia
// sesión y vuelve enseguida con msg: el resultado lo anota ese proceso.
func detached(env source.Env, id, tg, input, msg string, argv ...string) (string, error) {
	exe, err := executable()
	if err != nil {
		return "", err
	}
	if err := launch(exe, append([]string{"wifi-wait", id, tg, input, "--"}, argv...)...); err != nil {
		return "", err
	}
	return msg, nil
}

// Wait corre argv (solo nmcli u omarchy-network-band) hasta waitTimeout y
// anota el resultado en actions.log con el id, la fila y el dato de la
// acción que lo lanzó. Es `sysoscope wifi-wait`.
func Wait(env source.Env, id, tg, input string, argv []string) error {
	l := env.Lang
	if len(argv) == 0 || argv[0] != "nmcli" && argv[0] != "omarchy-network-band" {
		err := errors.New(i18n.T(l, "wifi-wait only runs nmcli or omarchy-network-band"))
		actlog.Write(env, id, tg, input, "", err)
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()
	out, err := env.Run.Run(ctx, argv[0], argv[1:]...)
	if err != nil {
		err = errors.New(Message(l, argv[0], out, err))
		actlog.Write(env, id, tg, input, "", err)
		return err
	}
	res := ""
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			res = line
			break
		}
	}
	actlog.Write(env, id, tg, input, res, nil)
	return nil
}

// LaunchDetached arranca name en su propia sesión (sobrevive al panel y a
// lazylazy), con stdin, stdout y stderr en /dev/null (nunca escribe en el
// stream), y no lo espera; una gorutina lo recoge si este proceso sigue vivo.
// La usan Wi-Fi y Bluetooth (bt-wait) para lanzar sus procesos desacoplados.
func LaunchDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// runInteractive corre name en la terminal de este proceso (nmtui desde la
// pantalla completa de lazylazy o desde la línea de comandos).
func runInteractive(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func stdinIsTTY() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
