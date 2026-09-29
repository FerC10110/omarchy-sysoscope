// Package bluetooth lee los dispositivos y los adaptadores de BlueZ (D-Bus)
// y el bloqueo de rfkill (/sys), y corre las acciones bt.* con los comandos
// de Omarchy.
package bluetooth

import (
	"cmp"
	"errors"
	"os"
	"slices"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

const (
	bluez     = "org.bluez"
	powerCmd  = "omarchy-bluetooth-power"
	deviceCmd = "omarchy-bluetooth-device"
)

// getenv lee el entorno (los tests la reemplazan).
var getenv = os.Getenv

// State es lo que se lee de BlueZ y de rfkill.
type State struct {
	Devices []model.BTDevice
	Present bool // hay algún org.bluez.Adapter1
	Powered bool // alguno está encendido
	Blocked bool // rfkill bloquea el Bluetooth (soft)
}

var errNotRunning = errors.New("bluez is not running")

// running pregunta al bus si BlueZ corre (NameHasOwner) antes de hablarle:
// una llamada a org.bluez sin dueño le pide a D-Bus que lo active y, con
// bluetooth.service deshabilitado, deja un "Activation via systemd failed"
// en el journal en cada muestra. Si la pregunta falla (p. ej. un bus de
// fixture sin esa respuesta), no corre: ningún camino debe activar BlueZ.
func running(env source.Env) bool {
	body, err := env.Bus.Call(true, "org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus.NameHasOwner", bluez)
	if err != nil || len(body) == 0 {
		return false
	}
	on, ok := body[0].(bool)
	return ok && on
}

// Read lee los adaptadores y los dispositivos de BlueZ y el bloqueo de
// rfkill. Error si BlueZ no corre o no responde (Blocked se lee igual).
func Read(env source.Env) (State, error) {
	st := State{Blocked: blocked(env.FS)}
	if !running(env) {
		return st, errNotRunning
	}
	objs, err := env.Bus.Objects(true, bluez, "/")
	if err != nil {
		return st, err
	}
	for _, ifs := range objs {
		if a, ok := ifs["org.bluez.Adapter1"]; ok {
			st.Present = true
			if p, _ := a["Powered"].(bool); p {
				st.Powered = true
			}
		}
		d, ok := ifs["org.bluez.Device1"]
		if !ok {
			continue
		}
		b := model.BTDevice{}
		b.Name, _ = d["Name"].(string)
		if b.Name == "" {
			b.Name, _ = d["Alias"].(string) // un cercano sin nombre anunciado
		}
		b.Address, _ = d["Address"].(string)
		b.Connected, _ = d["Connected"].(bool)
		b.Paired, _ = d["Paired"].(bool)
		b.Trusted, _ = d["Trusted"].(bool)
		if bat, ok := ifs["org.bluez.Battery1"]; ok {
			if f, ok := bat["Percentage"].(float64); ok {
				v := int(f)
				b.Battery = &v
			} else if u, ok := bat["Percentage"].(uint8); ok {
				v := int(u)
				b.Battery = &v
			}
		}
		st.Devices = append(st.Devices, b)
	}
	sortDevices(st.Devices)
	return st, nil
}

// sortDevices: conectados, emparejados y al final los cercanos; dentro de
// cada grupo por nombre y dirección (el mapa de D-Bus no tiene orden).
func sortDevices(ds []model.BTDevice) {
	rank := func(d model.BTDevice) int {
		switch {
		case d.Connected:
			return 0
		case d.Paired:
			return 1
		}
		return 2
	}
	slices.SortStableFunc(ds, func(a, b model.BTDevice) int {
		return cmp.Or(rank(a)-rank(b), strings.Compare(a.Name, b.Name), strings.Compare(a.Address, b.Address))
	})
}

// blocked: algún rfkill de tipo bluetooth con soft = 1.
func blocked(fs source.FS) bool {
	dirs, _ := fs.Glob("/sys/class/rfkill/rfkill*")
	for _, d := range dirs {
		t, _ := fs.Read(d + "/type")
		s, _ := fs.Read(d + "/soft")
		if strings.TrimSpace(t) == "bluetooth" && strings.TrimSpace(s) == "1" {
			return true
		}
	}
	return false
}

// Sample copia a s lo leído y si están los comandos de Omarchy. Sin BlueZ:
// sin dispositivos y "system.bluetooth" no disponible, como antes.
func Sample(env source.Env, s *model.System, r *collect.Report) {
	st, err := Read(env)
	if err != nil {
		r.Absent("system.bluetooth")
	}
	s.Bluetooth, s.BTPresent, s.BTPowered, s.BTBlocked = st.Devices, st.Present, st.Powered, st.Blocked
	path := getenv("PATH")
	s.BTTools = source.InPath(env.FS, path, powerCmd) && source.InPath(env.FS, path, deviceCmd)
}

// find busca un dispositivo por dirección, sin distinguir mayúsculas.
func find(ds []model.BTDevice, addr string) *model.BTDevice {
	for i := range ds {
		if strings.EqualFold(ds[i].Address, addr) {
			return &ds[i]
		}
	}
	return nil
}
