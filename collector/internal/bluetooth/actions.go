package bluetooth

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/actlog"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/wifi"
)

var (
	sectionIDs = []string{"bt.power", "bt.pair"}
	rowIDs     = []string{"bt.connect", "bt.disconnect", "bt.forget"}
)

// Known dice si id es una acción de Bluetooth.
func Known(id string) bool { return slices.Contains(sectionIDs, id) || slices.Contains(rowIDs, id) }

// IsSection dice si la acción es de toda la sección (no lleva fila).
func IsSection(id string) bool { return slices.Contains(sectionIDs, id) }

const (
	// actionTimeout: lazylazy corta una acción en segundo plano a los 5 s.
	actionTimeout = 4 * time.Second
	// waitTimeout es lo máximo que bt-wait espera al comando:
	// omarchy-bluetooth-device espera hasta 20 s a bluetoothctl, más prender
	// el adaptador.
	waitTimeout = 30 * time.Second
	// settle y pollEvery: después del comando, BlueZ puede tardar un poco en
	// mostrar el cambio; se relee cada pollEvery hasta settle.
	settle    = 2 * time.Second
	pollEvery = 250 * time.Millisecond
)

// Lo que lanza procesos o espera; los tests lo reemplazan.
var (
	launch     = wifi.LaunchDetached
	executable = os.Executable
	sleep      = time.Sleep
)

var addrRe = regexp.MustCompile(`^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$`)

// Run corre la acción de Bluetooth id sobre target (la dirección, o "" en
// las de sección) y la anota en actions.log. Las que usan los comandos de
// Omarchy vuelven enseguida: el resultado de verdad lo anota bt-wait.
func Run(env source.Env, id, target, input string) (string, error) {
	out, err := run(env, id, target)
	actlog.Write(env, id, target, input, out, err)
	return out, err
}

func run(env source.Env, id, target string) (string, error) {
	l := env.Lang
	if !Known(id) {
		return "", errors.New(i18n.F(l, "unknown action %q", id))
	}
	// Se lee ahora: el adaptador y los dispositivos pudieron cambiar.
	st, err := Read(env)
	if err != nil || !st.Present {
		return "", errors.New(i18n.T(l, "there is no Bluetooth adapter"))
	}
	path := getenv("PATH")
	switch id {
	case "bt.pair":
		if !source.InPath(env.FS, path, "omarchy-shell") {
			return "", errors.New(i18n.F(l, "requires %s", "omarchy-shell"))
		}
		if err := command(env, "omarchy-shell", "omarchy.bluetooth", "open"); err != nil {
			return "", err
		}
		return i18n.T(l, "Opened Omarchy's Bluetooth panel"), nil
	case "bt.power":
		if !source.InPath(env.FS, path, powerCmd) {
			return "", errors.New(i18n.F(l, "requires %s", powerCmd))
		}
		if st.Powered {
			return detached(env, id, "", i18n.T(l, "Turning Bluetooth off…"), powerCmd, "off")
		}
		return detached(env, id, "", i18n.T(l, "Turning Bluetooth on…"), powerCmd, "on")
	}
	if !addrRe.MatchString(target) {
		return "", errors.New(i18n.F(l, "%q is not a Bluetooth device", target))
	}
	d := find(st.Devices, target)
	if d == nil {
		return "", errors.New(i18n.F(l, "no Bluetooth device %s", target))
	}
	if !source.InPath(env.FS, path, deviceCmd) {
		return "", errors.New(i18n.F(l, "requires %s", deviceCmd))
	}
	name := label(d)
	switch id {
	case "bt.connect":
		if d.Connected {
			return "", errors.New(i18n.F(l, "already connected to %s", name))
		}
		if !d.Paired {
			return "", errors.New(i18n.F(l, "%s is not paired: pair it with e (Omarchy's Bluetooth panel)", name))
		}
		return detached(env, id, target, i18n.F(l, "Connecting to %s…", name), deviceCmd, "connect", d.Address)
	case "bt.disconnect":
		if !d.Connected {
			return "", errors.New(i18n.F(l, "%s is not connected", name))
		}
		return detached(env, id, target, i18n.F(l, "Disconnecting from %s…", name), deviceCmd, "disconnect", d.Address)
	}
	if !d.Paired {
		return "", errors.New(i18n.F(l, "%s is not paired", name))
	}
	return detached(env, id, target, i18n.F(l, "Forgetting %s…", name), deviceCmd, "forget", d.Address)
}

// label es el nombre para mostrar (sin caracteres de control: lo anuncia el
// dispositivo) o, si no tiene, la dirección.
func label(d *model.BTDevice) string {
	if n := wifi.Printable(strings.TrimSpace(d.Name)); n != "" {
		return n
	}
	return d.Address
}

// detached lanza `sysoscope bt-wait ID FILA -- ORDEN…` en su propia sesión
// y vuelve enseguida con msg: el resultado lo anota ese proceso.
func detached(env source.Env, id, tg, msg string, argv ...string) (string, error) {
	exe, err := executable()
	if err != nil {
		return "", err
	}
	if err := launch(exe, append([]string{"bt-wait", id, tg, "--"}, argv...)...); err != nil {
		return "", err
	}
	return msg, nil
}

// Wait corre argv (solo omarchy-bluetooth-device u omarchy-bluetooth-power)
// hasta waitTimeout sin fiarse de su salida (el de dispositivos siempre sale
// 0), relee BlueZ hasta settle esperando el estado que pidió la acción y
// anota en actions.log lo que pasó de verdad. Es `sysoscope bt-wait`.
func Wait(env source.Env, id, tg string, argv []string) error {
	out, err := wait(env, id, tg, argv)
	actlog.Write(env, id, tg, "", out, err)
	return err
}

func wait(env source.Env, id, tg string, argv []string) (string, error) {
	l := env.Lang
	if !Known(id) {
		return "", errors.New(i18n.F(l, "unknown action %q", id))
	}
	done, ok := goal(id, tg, argv)
	if !ok {
		return "", errors.New(i18n.T(l, "bt-wait only runs omarchy-bluetooth-device or omarchy-bluetooth-power"))
	}
	name := tg
	if before, err := Read(env); err == nil {
		if d := find(before.Devices, tg); d != nil {
			name = label(d)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	out, runErr := env.Run.Run(ctx, argv[0], argv[1:]...)
	cancel()
	if errors.Is(runErr, exec.ErrNotFound) {
		return "", errors.New(i18n.F(l, "requires %s", argv[0]))
	}
	// Lo que vale es el estado de BlueZ, no la salida del comando.
	reached := false
	for waited := time.Duration(0); ; waited += pollEvery {
		if st, err := Read(env); err == nil && done(st) {
			reached = true
			break
		}
		if waited >= settle {
			break
		}
		sleep(pollEvery)
	}
	good, bad := outcome(l, id, argv, name)
	if reached {
		return good, nil
	}
	// El estado no llegó: si además la orden falló de verdad (venció el plazo
	// o salió mal), se suma el motivo real al mensaje.
	if runErr != nil {
		bad = i18n.F(l, "%s: %s", bad, wifi.Message(l, argv[0], out, runErr))
	}
	return "", errors.New(bad)
}

// verb es el argumento que espera omarchy-bluetooth-device para cada acción
// de fila.
var verb = map[string]string{"bt.connect": "connect", "bt.disconnect": "disconnect", "bt.forget": "forget"}

// goal es lo que BlueZ tiene que mostrar para que la acción haya salido.
// ok = false si argv no es exactamente la orden que bt-wait corre para esa
// acción y esa fila (así el resultado de una no confirma el de otra).
func goal(id, tg string, argv []string) (func(State) bool, bool) {
	dev := func(st State) *model.BTDevice { return find(st.Devices, tg) }
	switch id {
	case "bt.power":
		if len(argv) != 2 || argv[0] != powerCmd {
			return nil, false
		}
		switch argv[1] {
		case "on":
			return func(st State) bool { return st.Powered }, true
		case "off":
			return func(st State) bool { return !st.Powered }, true
		}
	case "bt.connect", "bt.disconnect", "bt.forget":
		if len(argv) != 3 || argv[0] != deviceCmd || argv[1] != verb[id] ||
			!addrRe.MatchString(argv[2]) || !strings.EqualFold(argv[2], tg) {
			return nil, false
		}
		switch id {
		case "bt.connect":
			return func(st State) bool { d := dev(st); return d != nil && d.Connected }, true
		case "bt.disconnect":
			return func(st State) bool { d := dev(st); return d == nil || !d.Connected }, true
		default:
			return func(st State) bool { d := dev(st); return d == nil || !d.Paired }, true
		}
	}
	return nil, false
}

// outcome son los textos de éxito y de fracaso de la acción (sin el motivo:
// wait lo suma si la orden falló de verdad).
func outcome(l, id string, argv []string, name string) (good, bad string) {
	switch {
	case id == "bt.power" && argv[1] == "on":
		return i18n.T(l, "Bluetooth turned on"), i18n.T(l, "Could not turn Bluetooth on")
	case id == "bt.power":
		return i18n.T(l, "Bluetooth turned off"), i18n.T(l, "Could not turn Bluetooth off")
	case id == "bt.connect":
		return i18n.F(l, "Connected to %s", name), i18n.F(l, "Could not connect to %s", name)
	case id == "bt.disconnect":
		return i18n.F(l, "Disconnected from %s", name), i18n.F(l, "Could not disconnect from %s", name)
	default:
		return i18n.F(l, "Forgot %s", name), i18n.F(l, "Could not forget %s", name)
	}
}

// command corre una orden corta (hasta actionTimeout) y devuelve su error legible.
func command(env source.Env, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
	defer cancel()
	out, err := env.Run.Run(ctx, name, args...)
	if err != nil {
		return errors.New(wifi.Message(env.Lang, name, out, err))
	}
	return nil
}
