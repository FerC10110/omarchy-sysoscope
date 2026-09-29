package wifi

import "github.com/FerC10110/omarchy-sysoscope/collector/internal/source"

// Rfkill es el bloqueo de la placa según /sys/class/rfkill: "hard" (tecla o
// interruptor), "soft" (apagada por software) u "on". "" si la placa no tiene
// rfkill o no se sabe su phy. No corre comandos.
func Rfkill(fs source.FS, iface string) string {
	phy, err := fs.Read("/sys/class/net/" + iface + "/phy80211/name")
	if err != nil || phy == "" {
		return ""
	}
	dirs, _ := fs.Dir("/sys/class/rfkill")
	for _, d := range dirs {
		p := "/sys/class/rfkill/" + d + "/"
		typ, _ := fs.Read(p + "type")
		name, _ := fs.Read(p + "name")
		if typ != "wlan" || name != phy {
			continue
		}
		if h, _ := fs.Read(p + "hard"); h == "1" {
			return "hard"
		}
		if s, _ := fs.Read(p + "soft"); s == "1" {
			return "soft"
		}
		return "on"
	}
	return ""
}

// Radio combina rfkill con el Wi-Fi de NetworkManager (un solo interruptor
// para todas las placas): hard gana; soft si rfkill o NetworkManager la
// apagan; si no, on.
func Radio(fs source.FS, iface string, nmOn bool) string {
	switch Rfkill(fs, iface) {
	case "hard":
		return "hard"
	case "soft":
		return "soft"
	}
	if !nmOn {
		return "soft"
	}
	return "on"
}

// Ifaces son las placas Wi-Fi de /sys/class/net (las que tienen wireless/),
// ordenadas por nombre.
func Ifaces(fs source.FS) []string {
	names, _ := fs.Dir("/sys/class/net")
	var out []string
	for _, n := range names {
		if fs.Exists("/sys/class/net/" + n + "/wireless") {
			out = append(out, n)
		}
	}
	return out
}
