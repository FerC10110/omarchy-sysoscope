package security

import (
	"path"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// dmDev es un dispositivo device-mapper de /sys/block/dm-*.
type dmDev struct {
	name, uuid string
	slaves     []string
}

// maxDepth corta una cadena de slaves que no termina (un ciclo).
const maxDepth = 8

func readDM(env source.Env) map[string]dmDev {
	devs := map[string]dmDev{}
	paths, _ := env.FS.Glob("/sys/block/dm-*")
	for _, p := range paths {
		name, _ := env.FS.Read(p + "/dm/name")
		uuid, _ := env.FS.Read(p + "/dm/uuid")
		slaves, _ := env.FS.Dir(p + "/slaves")
		devs[path.Base(p)] = dmDev{name: strings.TrimSpace(name), uuid: strings.TrimSpace(uuid), slaves: slaves}
	}
	return devs
}

// Crypt dice qué sistemas de archivos y qué swaps activas están cifrados,
// sin comandos: el uuid de cada dm con prefijo CRYPT-<TIPO>- y sus slaves
// (se siguen hasta la partición física). Cifrado, algoritmo y slots de LUKS
// piden root y no se leen. Una swap archivo es lo que sea el sistema de
// archivos que la contiene; zram está en memoria.
func Crypt(env source.Env, fss []model.Filesystem, swaps []model.Swap) ([]model.CryptVolume, []model.SwapCrypt) {
	devs := readDM(env)
	var vols []model.CryptVolume
	for _, f := range fss {
		v := model.CryptVolume{Device: f.Device, Mounts: f.Mounts}
		t, on, enc, found := cryptState(f.Device, devs)
		switch {
		case found:
			v.Encrypted, v.Type, v.On = enc, t, on
		default:
			// sysfs no resuelve el dispositivo: vale lo que dijo lsblk (sin tipo).
			v.Encrypted = f.Crypt
		}
		vols = append(vols, v)
	}
	var sw []model.SwapCrypt
	for _, s := range swaps {
		c := model.SwapCrypt{Name: s.Name, Kind: s.Kind, State: "plain"}
		switch s.Kind {
		case "zram":
			c.State = "memory"
		case "file":
			v, ok := holder(s.Name, vols)
			switch {
			case !ok:
				c.State = "unknown"
			case v.Encrypted:
				c.State, c.Type, c.On = "encrypted", v.Type, v.On
			}
		default:
			if t, on, enc, _ := cryptState(s.Name, devs); enc {
				c.State, c.Type, c.On = "encrypted", t, on
			}
		}
		sw = append(sw, c)
	}
	return vols, sw
}

// cryptState resuelve dev en sysfs: found dice si es un dm conocido (y
// entonces enc es la respuesta definitiva, aunque lsblk diga TYPE crypt:
// lsblk lo dice también de dm-verity, dm-integrity y de una LVM mixta);
// sin found, quien llama decide con lo que tenga (el Crypt de lsblk).
func cryptState(dev string, devs map[string]dmDev) (typ, on string, enc, found bool) {
	k := devKey(dev, devs)
	if _, ok := devs[k]; k == "" || !ok {
		return "", "", false, false
	}
	typ, on, enc = cryptOf(k, devs, 0)
	return typ, on, enc, true
}

// cryptOf dice si el dm k está cifrado: él mismo (un tipo de cryptTypes en
// su uuid) o, si no lo es, todas sus ramas de abajo — una LVM sobre varios
// discos es cifrada solo si TODOS lo están (un "Encrypted" falso es el peor
// resultado que puede dar esto). Devuelve el tipo y la partición física de
// abajo (physical); una capa que no es cifrado (dm-verity, dm-integrity) se
// atraviesa igual que cualquier otra, sin marcar nada por sí sola.
func cryptOf(k string, devs map[string]dmDev, depth int) (string, string, bool) {
	d, ok := devs[k]
	if !ok || depth > maxDepth {
		return "", "", false
	}
	if t, ok := cryptType(d.uuid); ok {
		return t, physical(d.slaves, devs, depth), true
	}
	if len(d.slaves) == 0 {
		return "", "", false
	}
	var typ string
	var ons []string
	for _, s := range d.slaves {
		t, on, ok := cryptOf(s, devs, depth+1)
		if !ok {
			return "", "", false
		}
		if typ == "" {
			typ = t
		}
		if on != "" {
			ons = append(ons, on)
		}
	}
	return typ, strings.Join(ons, ", "), true
}

// cryptTypes son los tipos de cifrado que dm-crypt informa en el uuid
// ("CRYPT-<TIPO>-..."); otros valores con el mismo prefijo (VERITY,
// INTEGRITY) son capas de integridad, no de cifrado, y no cuentan como
// "Encrypted": cryptOf sigue bajando por sus slaves como con cualquier LVM.
var cryptTypes = map[string]bool{"LUKS1": true, "LUKS2": true, "PLAIN": true, "LOOPAES": true, "TCRYPT": true, "BITLK": true}

// cryptType: "CRYPT-LUKS2-<hex>-root" → "LUKS2", solo si el tipo está en cryptTypes.
func cryptType(uuid string) (string, bool) {
	rest, ok := strings.CutPrefix(uuid, "CRYPT-")
	if !ok {
		return "", false
	}
	t, _, ok := strings.Cut(rest, "-")
	if !ok || !cryptTypes[t] {
		return "", false
	}
	return t, true
}

// physical sigue los slaves hasta la partición física de abajo: uno que no
// es un dm (una partición) se lista tal cual; uno que sí lo es (LUKS sobre
// LVM, una capa de integridad bajo LUKS) se resuelve recursivamente. Varias
// ramas (una LVM sobre varios discos) se juntan con ", ".
func physical(slaves []string, devs map[string]dmDev, depth int) string {
	var names []string
	for _, s := range slaves {
		d, isDM := devs[s]
		switch {
		case !isDM:
			names = append(names, s)
		case depth < maxDepth:
			if p := physical(d.slaves, devs, depth+1); p != "" {
				names = append(names, p)
			}
		}
	}
	return strings.Join(names, ", ")
}

// devKey pasa "/dev/mapper/root" o "/dev/dm-0" al nombre en /sys/block.
func devKey(dev string, devs map[string]dmDev) string {
	if k, ok := strings.CutPrefix(dev, "/dev/"); ok && strings.HasPrefix(k, "dm-") {
		return k
	}
	if n, ok := strings.CutPrefix(dev, "/dev/mapper/"); ok {
		for k, d := range devs {
			if d.name == n {
				return k
			}
		}
	}
	return ""
}

// holder es el volumen con el montaje más largo que contiene p (una ruta
// de archivo): "/home" contiene "/home/swapfile" pero no "/homework/x".
func holder(p string, vols []model.CryptVolume) (model.CryptVolume, bool) {
	var best model.CryptVolume
	n, ok := -1, false
	for _, v := range vols {
		for _, m := range v.Mounts {
			if (m == "/" || p == m || strings.HasPrefix(p, m+"/")) && len(m) > n {
				best, n, ok = v, len(m), true
			}
		}
	}
	return best, ok
}
