// Package ids traduce IDs de PCI y USB a nombres con las bases de hwdata
// (pci.ids, usb.ids). Lee el archivo en streaming hasta el bloque del vendor
// buscado, sin cargarlo entero, y guarda los resultados.
package ids

import (
	"bufio"
	"os"
	"strings"
	"sync"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

var (
	mu    sync.Mutex
	cache = map[string][2]string{}
)

// PCI devuelve el nombre del vendor y del dispositivo ("" si no se conocen).
func PCI(env source.Env, vendor, device string) (string, string) {
	return lookup(env, []string{"/usr/share/hwdata/pci.ids", "/usr/share/misc/pci.ids"}, vendor, device)
}

// USB es PCI para usb.ids.
func USB(env source.Env, vendor, product string) (string, string) {
	return lookup(env, []string{"/usr/share/hwdata/usb.ids", "/usr/share/misc/usb.ids"}, vendor, product)
}

func norm(id string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(id), "0x"))
}

func lookup(env source.Env, files []string, vendor, device string) (string, string) {
	vendor, device = norm(vendor), norm(device)
	for _, f := range files {
		p := env.FS.Real(f)
		key := p + "|" + vendor + "|" + device
		mu.Lock()
		v, ok := cache[key]
		mu.Unlock()
		if ok {
			return v[0], v[1]
		}
		fh, err := os.Open(p)
		if err != nil {
			continue
		}
		vn, dn := scan(fh, vendor, device)
		fh.Close()
		mu.Lock()
		cache[key] = [2]string{vn, dn}
		mu.Unlock()
		return vn, dn
	}
	return "", ""
}

func scan(fh *os.File, vendor, device string) (string, string) {
	sc := bufio.NewScanner(fh)
	vn := ""
	for sc.Scan() {
		l := sc.Text()
		if l == "" || l[0] == '#' {
			continue
		}
		if l[0] != '\t' {
			if vn != "" {
				return vn, "" // terminó el bloque del vendor sin encontrar el dispositivo
			}
			id, name, ok := strings.Cut(l, "  ")
			if ok && strings.ToLower(id) == vendor {
				vn = strings.TrimSpace(name)
			}
			continue
		}
		if vn == "" || strings.HasPrefix(l, "\t\t") {
			continue
		}
		id, name, ok := strings.Cut(l[1:], "  ")
		if ok && strings.ToLower(id) == device {
			return vn, strings.TrimSpace(name)
		}
	}
	return vn, ""
}
