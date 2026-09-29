// Package fixture captura una máquina como fixture de tests (archivos de
// /proc y /sys, salidas de comandos y respuestas de D-Bus), opcionalmente
// anonimizada, para que cualquiera pueda aportar su hardware.
package fixture

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/gpu"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/maint"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/mem"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/power"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/security"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/storage"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/system"
)

// Globs son los archivos que leen los colectores. La lista está documentada
// en el README para quien quiera revisar qué se captura.
var Globs = []string{
	"/proc/cpuinfo", "/proc/stat", "/proc/meminfo", "/proc/loadavg", "/proc/uptime", "/proc/swaps", "/proc/vmstat",
	"/proc/diskstats", "/proc/net/dev", "/proc/net/route", "/proc/net/wireless", "/proc/asound/cards",
	"/proc/pressure/*", "/proc/sys/kernel/arch", "/proc/sys/kernel/osrelease", "/proc/sys/kernel/hostname",
	"/proc/sys/kernel/random/boot_id", "/proc/driver/nvidia/version",
	"/etc/os-release", "/etc/hostname", "/etc/resolv.conf", "/run/systemd/resolve/resolv.conf",
	"/sys/devices/system/cpu/online", "/sys/devices/system/cpu/cpufreq/boost",
	"/sys/devices/system/cpu/intel_pstate/no_turbo", "/sys/devices/system/cpu/vulnerabilities/*",
	"/sys/devices/system/cpu/cpu*/topology/{core_id,die_id,physical_package_id,thread_siblings_list}",
	"/sys/devices/system/cpu/cpu*/cache/index*/{level,type,size,shared_cpu_list}",
	"/sys/devices/system/cpu/cpu*/cpufreq/{scaling_driver,scaling_governor,cpuinfo_max_freq,cpuinfo_min_freq,scaling_cur_freq,base_frequency,energy_performance_preference}",
	"/sys/class/hwmon/hwmon*/{name,temp*_input,temp*_label,temp*_max,temp*_crit,fan*_input,fan*_label,in*_input,in*_label,device/model}",
	"/sys/block/zram*/{comp_algorithm,mm_stat}",
	"/sys/class/drm/card*/device/{uevent,vendor,device,current_link_speed,max_link_speed,current_link_width,max_link_width,boot_vga,vbios_version,gpu_busy_percent,mem_busy_percent,mem_info_vram_used,mem_info_vram_total,mem_info_gtt_used,mem_info_gtt_total}",
	"/sys/class/drm/card*/device/power/{runtime_status,control}",
	"/sys/class/drm/card*/device/hwmon/hwmon*/{name,temp*_input,temp*_label,power1_average,power1_input}",
	"/sys/class/drm/card*-*/{status,edid}",
	"/sys/class/net/*/{type,operstate,flags,address,speed}",
	"/sys/class/power_supply/*/{type,status,capacity,energy_now,energy_full,energy_full_design,charge_now,charge_full,charge_full_design,power_now,current_now,voltage_now,voltage_min_design,cycle_count,charge_control_end_threshold,model_name,manufacturer,scope,online}",
	"/sys/class/powercap/intel-rapl:*/{name,max_energy_range_uj,energy_uj}",
	"/sys/class/dmi/id/{bios_vendor,bios_version,bios_date,bios_release,board_vendor,board_name,board_version,product_name,product_family,product_version,sys_vendor,chassis_type}",
	"/sys/bus/pci/devices/*/{class,vendor,device}",
	"/sys/bus/usb/devices/*/{idVendor,idProduct,product,manufacturer,speed}",
	"/sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c",
	"/sys/class/tpm/tpm0/tpm_version_major",
	"/sys/class/rfkill/rfkill*/{type,soft,hard}",
	"/var/lib/pacman/local/omarchy*/desc",
	// Mantenimiento: pacman.log (reducido con --anonymize), el CacheDir de
	// pacman, el driver NVIDIA cargado y los configs de snapper.
	maint.PacmanLog, "/etc/pacman.conf", "/sys/module/nvidia/version",
	"/etc/conf.d/snapper", "/etc/snapper/configs/*", "/usr/share/dbus-1/system-services/org.opensuse.Snapper.service",
	// Seguridad: las tablas de sockets, el rango efímero, los archivos de
	// ufw y el device-mapper (cifrado). /etc/passwd no va acá: se escribe
	// reducido en (*capture).security.
	"/proc/net/{tcp,tcp6,udp,udp6}", security.PortRange,
	security.UFWConf, security.UFWDefault, security.UFWRules, security.UFWRules6,
	"/sys/block/dm-*/dm/{uuid,name}",
	// Energía y pantallas: el modo de suspensión, qué puede despertar el
	// equipo y la versión, la clase y el puerto de cada USB.
	"/sys/power/mem_sleep", "/sys/bus/usb/devices/*/{version,bDeviceClass}", "/sys/bus/usb/devices/*/power/wakeup",
	"/sys/bus/usb/devices/*/*:1.0/bInterfaceClass", "/sys/bus/pci/devices/*/power/wakeup",
}

// expand resuelve un nivel de llaves {a,b} (filepath.Glob no las entiende).
func expand(g string) []string {
	i := strings.Index(g, "{")
	j := strings.Index(g, "}")
	if i < 0 || j < i {
		return []string{g}
	}
	var out []string
	for _, alt := range strings.Split(g[i+1:j], ",") {
		out = append(out, expand(g[:i]+alt+g[j+1:])...)
	}
	return out
}

type capture struct {
	env    source.Env
	out    string
	anon   *Anonymizer
	crypts int // CRYPT-… y LVM-… anonimizados
	// Textos libres de Energía anonimizados: el mismo texto da siempre el
	// mismo número ("reason N" de logind, "title N" de las ventanas).
	reasons, titles map[string]int
}

// Capture guarda en out (root/, cmd/, dbus.json, timex) lo que leen los
// colectores de env. Con anonymize reemplaza MACs, series, UUIDs, SSIDs,
// IPs, hostname y la ruta del home.
func Capture(env source.Env, out string, anonymize bool) error {
	c := &capture{env: env, out: out, reasons: map[string]int{}, titles: map[string]int{}}
	if anonymize {
		host, _ := env.FS.Read("/etc/hostname")
		c.anon = NewAnonymizer(os.Getenv("USER"), env.Home, strings.TrimSpace(host))
		c.registerOtherDMIDs() // antes de leer nada: también vale para lsblk y files()
	}
	outs := c.commands() // lsblk y D-Bus registran los números de serie
	dbus := c.dbus()
	for key, o := range outs {
		c.write("cmd/"+key+".txt", []byte(c.text(string(o.out))))
		if o.code > 0 {
			c.write("cmd/"+key+".exit", []byte(fmt.Sprintf("%d\n", o.code)))
		}
	}
	if err := c.files(); err != nil {
		return err
	}
	c.markers()
	c.maintenance()
	c.security()
	c.energy()
	c.clock()
	data, err := json.MarshalIndent(dbus, "", " ")
	if err != nil {
		return err
	}
	return c.write("dbus.json", []byte(c.text(string(data))))
}

func (c *capture) text(s string) string {
	if c.anon == nil {
		return s
	}
	return c.anon.Text(s)
}

func (c *capture) write(rel string, data []byte) error {
	p := filepath.Join(c.out, c.text(rel)) // las rutas también llevan MACs (hid-<mac>-battery)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

// pacmanLogTailCap es la cola de pacman.log que se guarda sin anonimizar
// cuando el archivo pasa el tope general: alcanza para la última
// actualización (ver maint.tailBytes, el mismo tamaño que lee Mantenimiento).
const pacmanLogTailCap = 256 << 10 // 256 KiB

func (c *capture) files() error {
	for _, g := range Globs {
		for _, pat := range expand(g) {
			paths, _ := c.env.FS.Glob(pat)
			for _, p := range paths {
				real := c.env.FS.Real(p)
				st, err := os.Stat(real)
				if err != nil || st.IsDir() {
					continue
				}
				var data []byte
				switch {
				case st.Size() <= 2<<20:
					data, err = os.ReadFile(real)
				case p == maint.PacmanLog && c.anon != nil:
					data, err = os.ReadFile(real) // pacmanLog lo reduce mucho: se lee entero
				case p == maint.PacmanLog:
					data, err = readTail(real, st.Size(), pacmanLogTailCap)
				default:
					continue // demasiado grande, no vale la pena
				}
				if err != nil {
					continue // sin permiso (p. ej. energy_uj): el colector lo ve igual en la máquina real
				}
				if err := c.write("root"+p, c.fileContent(p, data)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// readTail lee los últimos max bytes de p (cap el archivo entero si es más
// chico) y descarta la primera línea si quedó partida, para no escribir
// basura al principio.
func readTail(p string, size, max int64) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	n := min(max, size)
	buf := make([]byte, n)
	if _, err := f.ReadAt(buf, size-n); err != nil && err != io.EOF {
		return nil, err
	}
	if n < size {
		if i := bytes.IndexByte(buf, '\n'); i >= 0 {
			buf = buf[i+1:]
		} else {
			buf = nil
		}
	}
	return buf, nil
}

func (c *capture) fileContent(p string, data []byte) []byte {
	if strings.HasPrefix(p, "/proc/net/tcp") || strings.HasPrefix(p, "/proc/net/udp") {
		// Siempre (también sin anonymize): solo lo que Seguridad lee.
		data = listenersOnly(data, strings.HasPrefix(p, "/proc/net/tcp"))
	}
	if c.anon == nil {
		return data
	}
	switch {
	case strings.HasSuffix(p, "/edid"):
		return EDID(data)
	case strings.Contains(p, "/efivars/"):
		return data
	case p == "/etc/hostname" || p == "/proc/sys/kernel/hostname":
		return []byte("host\n")
	case strings.HasSuffix(p, "resolv.conf"):
		return []byte(c.anon.IPs(string(data)))
	case p == maint.PacmanLog:
		return c.pacmanLog(data)
	case p == "/etc/pacman.conf":
		return pacmanConf(data)
	case p == "/etc/conf.d/snapper" || strings.HasPrefix(p, "/etc/snapper/configs/"):
		return c.snapperConfig(data)
	case strings.HasPrefix(p, "/proc/net/tcp") || strings.HasPrefix(p, "/proc/net/udp"):
		return c.procNet(data)
	case p == security.UFWRules || p == security.UFWRules6:
		return c.ufwRules(data)
	case strings.HasSuffix(p, "/dm/uuid"):
		return c.dmUUID(data)
	case p == "/proc/net/route":
		lines := strings.SplitAfter(string(data), "\n")
		for i, l := range lines {
			f := strings.Split(l, "\t")
			if i > 0 && len(f) > 2 {
				f[1], f[2] = c.anon.RouteHex(f[1]), c.anon.RouteHex(f[2])
				lines[i] = strings.Join(f, "\t")
			}
		}
		return []byte(strings.Join(lines, ""))
	}
	return []byte(c.anon.Text(string(data)))
}

// markers recrea lo que los colectores miran por existencia o por symlink:
// device/wireless/bridge de las interfaces, drivers, paquetes y flatpaks.
func (c *capture) markers() {
	nets, _ := c.env.FS.Glob("/sys/class/net/*")
	for _, n := range nets {
		for _, m := range []string{"device", "wireless", "bridge"} {
			if c.env.FS.Exists(n + "/" + m) {
				c.write("root"+n+"/"+m+"/.keep", nil)
			}
		}
	}
	link := func(p string) {
		if l, err := c.env.FS.Link(p); err == nil {
			dst := filepath.Join(c.out, "root", p)
			os.MkdirAll(filepath.Dir(dst), 0o755)
			os.WriteFile(dst+".link", []byte("drivers/"+path.Base(l)+"\n"), 0o644) // sin symlinks: ver FS.Link
		}
	}
	pcis, _ := c.env.FS.Glob("/sys/bus/pci/devices/*")
	for _, d := range pcis {
		link(d + "/driver")
	}
	usbs, _ := c.env.FS.Glob("/sys/bus/usb/devices/*")
	for _, d := range usbs {
		link(d + "/" + path.Base(d) + ":1.0/driver")
	}
	// Los colectores cuentan paquetes y flatpaks; con anonymize se guardan
	// la cantidad, los de Omarchy (de ahí sale su versión) y el de espacio de
	// usuario de NVIDIA (su versión se compara con el driver cargado), no la
	// lista.
	pkgs, _ := c.env.FS.Dir("/var/lib/pacman/local")
	n := 0
	for _, p := range pkgs {
		if st, err := os.Stat(c.env.FS.Real("/var/lib/pacman/local/" + p)); err == nil && st.IsDir() {
			if c.anon != nil && !strings.HasPrefix(p, "omarchy") && !maint.IsNVIDIAUtils(p) {
				n++
				p = fmt.Sprintf("pkg%d-1-1", n)
			}
			os.MkdirAll(filepath.Join(c.out, "root/var/lib/pacman/local", p), 0o755)
		}
	}
	for dir, dst := range map[string]string{"/var/lib/flatpak/app": "/var/lib/flatpak/app",
		c.env.Home + "/.local/share/flatpak/app": "/home/user/.local/share/flatpak/app"} {
		apps, _ := c.env.FS.Dir(dir)
		for i, a := range apps {
			if c.anon != nil {
				a = fmt.Sprintf("org.example.App%d", i+1)
			}
			os.MkdirAll(filepath.Join(c.out, "root", dst, a), 0o755)
		}
	}
}

// clock guarda lo que lee system.clock fuera de D-Bus: el destino de
// /etc/localtime (como .link: sin symlinks), /etc/timezone y la palabra de
// estado de adjtimex en "timex". La zona dice dónde vive el dueño: con
// anonymize, UTC.
func (c *capture) clock() {
	if l, err := c.env.FS.Link("/etc/localtime"); err == nil {
		if c.anon != nil {
			l = "/usr/share/zoneinfo/UTC"
		}
		c.write("root/etc/localtime.link", []byte(l+"\n"))
	}
	if z, err := c.env.FS.Read("/etc/timezone"); err == nil {
		if c.anon != nil {
			z = "UTC"
		}
		c.write("root/etc/timezone", []byte(z+"\n"))
	}
	if c.env.Timex != nil {
		if st, err := c.env.Timex(); err == nil {
			c.write("timex", []byte(fmt.Sprintf("%d\n", st)))
		}
	}
}

type cmdOut struct {
	out  []byte
	code int
}

func (c *capture) commands() map[string]cmdOut {
	res := map[string]cmdOut{}
	cmds := [][]string{
		append([]string{"lsblk"}, storage.LsblkArgs...),
		{"inxi", "-m", "-c", "0"},
		append([]string{"nvidia-smi"}, gpu.SMIArgs...),
		append([]string{"nvidia-smi"}, gpu.SMIProcArgs...),
		{"systemd-analyze", "time"},
		{"pacman", "-Qqm"},
		{"pactl", "--format=json", "list", "sinks"},
		{"pactl", "--format=json", "list", "sources"},
		{"pactl", "--format=json", "info"},
		{"omarchy-audio-sink-availability"},
		{"journalctl", "--disk-usage"},
		// Salud (se leen con Mantenimiento abierta): los errores de este
		// arranque, los volcados de 7 días y lo que mató systemd-oomd.
		append([]string{"journalctl"}, maint.JournalErrArgs...),
		append([]string{"coredumpctl"}, maint.CoredumpArgs...),
		append([]string{"journalctl"}, maint.OomdArgs...),
	}
	nets, _ := c.env.FS.Glob("/sys/class/net/*")
	for _, n := range nets {
		if c.env.FS.Exists(n + "/wireless") {
			cmds = append(cmds, []string{"iw", "dev", path.Base(n), "link"})
		}
	}
	// El journal del kernel solo con muertes, como el colector (~49 ms).
	if n := mem.OOMKills(c.env); n != nil && *n > 0 {
		cmds = append(cmds, append([]string{"journalctl"}, maint.KernelOOMArgs...))
	}
	for _, cmd := range cmds {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		out, err := c.env.Run.Run(ctx, cmd[0], cmd[1:]...)
		cancel()
		if out == nil && err != nil && (cmd[0] == "journalctl" || cmd[0] == "coredumpctl") && maint.NoEntries(out, err) {
			out = []byte{} // sin entradas o sin volcados: sale ≠0 sin escribir nada (mismo criterio que maint.NoEntries)
		}
		if errors.Is(err, exec.ErrNotFound) || (err != nil && out == nil) {
			continue
		}
		if cmd[0] == "lsblk" && c.anon != nil {
			c.lsblkSerials(out)
		}
		if cmd[0] == "pacman" && c.anon != nil { // solo importa la cantidad
			var b strings.Builder
			for i := range strings.Fields(string(out)) {
				fmt.Fprintf(&b, "aur%d\n", i+1)
			}
			out = []byte(b.String())
		}
		if cmd[0] == "pactl" {
			out = c.pactl(cmd[1:], out)
		}
		switch {
		case cmd[0] == "journalctl" && slices.Contains(cmd, "json"):
			out = c.journal(cmd[1:], out)
		case cmd[0] == "coredumpctl":
			out = c.coredumps(out)
		}
		res[source.Key(cmd[0], cmd[1:]...)] = cmdOut{out, source.ExitStatus(err)}
	}
	return res
}

func (c *capture) lsblkSerials(out []byte) {
	var tree struct {
		Blockdevices []struct {
			Serial *string `json:"serial"`
		} `json:"blockdevices"`
	}
	if json.Unmarshal(out, &tree) == nil {
		for _, d := range tree.Blockdevices {
			if d.Serial != nil {
				c.anon.Serial(*d.Serial)
			}
		}
	}
}

// btMAC es la MAC de un nodo Bluetooth de pactl (bluez_output.AA_BB_…).
var btMAC = regexp.MustCompile(`(?i)[0-9a-f]{2}(_[0-9a-f]{2}){5}`)

// pactl deja de la salida de pactl solo lo que lee audio.Reader: en info,
// las dos predeterminadas (sin usuario, host ni cookie); en las listas,
// nombre, descripción, silencio, volumen, puertos y tres propiedades. Con
// anonymize, un nodo Bluetooth pierde su nombre (device<N>) y su MAC (se
// registra aquí y Text la reemplaza al escribir, también con "_").
func (c *capture) pactl(args []string, out []byte) []byte {
	if args[len(args)-1] == "info" {
		var d map[string]any
		if json.Unmarshal(out, &d) != nil {
			return out
		}
		b, _ := json.Marshal(map[string]any{"default_sink_name": d["default_sink_name"], "default_source_name": d["default_source_name"]})
		return b
	}
	var ns []map[string]any
	if json.Unmarshal(out, &ns) != nil {
		return out
	}
	res := make([]map[string]any, 0, len(ns))
	for _, n := range ns {
		m := map[string]any{}
		for _, k := range []string{"name", "description", "mute", "volume", "ports"} {
			if v, ok := n[k]; ok {
				m[k] = v
			}
		}
		p := map[string]any{}
		if src, ok := n["properties"].(map[string]any); ok {
			for _, k := range []string{"object.id", "node.nick", "device.class"} {
				if v, ok := src[k]; ok {
					p[k] = v
				}
			}
		}
		m["properties"] = p
		if name, _ := m["name"].(string); c.anon != nil && strings.HasPrefix(name, "bluez_") {
			if mac := btMAC.FindString(name); mac != "" {
				c.anon.Text(strings.ReplaceAll(mac, "_", ":"))
			}
			desc, _ := m["description"].(string)
			anon := c.anon.Name(desc)
			m["description"] = anon
			if _, ok := p["node.nick"]; ok {
				p["node.nick"] = anon
			}
		}
		res = append(res, m)
	}
	b, _ := json.Marshal(res)
	return b
}

// dbus junta las respuestas de D-Bus que usan los colectores, con las
// claves del FakeBus.
func (c *capture) dbus() map[string]map[string]any {
	sys, ses := map[string]any{}, map[string]any{}
	bus := c.env.Bus
	keepUdisks := map[string][]string{
		"org.freedesktop.UDisks2.Drive":           {"Model", "Serial", "Size"},
		"org.freedesktop.UDisks2.Drive.Ata":       {"SmartSupported", "SmartEnabled", "SmartFailing", "SmartPowerOnSeconds", "SmartTemperature", "SmartNumBadSectors", "SmartUpdated"},
		"org.freedesktop.UDisks2.NVMe.Controller": {"SmartCriticalWarning", "SmartPowerOnHours", "SmartTemperature", "SmartUpdated"},
	}
	if objs, err := bus.Objects(true, "org.freedesktop.UDisks2", "/org/freedesktop/UDisks2"); err == nil {
		f := filter(objs, keepUdisks)
		paths := make([]string, 0, len(f))
		for p := range f {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			ifs := f[p]
			if s, ok := ifs["org.freedesktop.UDisks2.Drive"]["Serial"].(string); ok && c.anon != nil {
				c.anon.Serial(s)
			}
			if _, ok := ifs["org.freedesktop.UDisks2.NVMe.Controller"]; ok {
				method := "org.freedesktop.UDisks2.NVMe.Controller.SmartGetAttributes"
				arg := map[string]any{}
				if body, err := bus.Call(true, "org.freedesktop.UDisks2", p, method, arg); err == nil {
					sys[fmt.Sprintf("call org.freedesktop.UDisks2 %s %s %v", p, method, arg)] = body
				}
			}
		}
		sys["objects org.freedesktop.UDisks2 /org/freedesktop/UDisks2"] = f
	}
	for _, prop := range []string{"ActiveProfile", "Profiles"} {
		if v, err := bus.Property(true, "net.hadess.PowerProfiles", "/net/hadess/PowerProfiles", "net.hadess.PowerProfiles", prop); err == nil {
			sys["net.hadess.PowerProfiles /net/hadess/PowerProfiles net.hadess.PowerProfiles "+prop] = v
		}
	}
	// El reloj y el firewall: las unidades de system.WatchedUnits en la
	// misma llamada que hace el colector (no timedate1, que se activa por
	// D-Bus y se cierra enseguida; ver system.clock).
	sd, sdPath, ntp := "org.freedesktop.systemd1", "/org/freedesktop/systemd1", "systemd-timesyncd.service"
	byNames, fileState := "org.freedesktop.systemd1.Manager.ListUnitsByNames", "org.freedesktop.systemd1.Manager.GetUnitFileState"
	if body, err := bus.Call(true, sd, sdPath, byNames, system.WatchedUnits); err == nil {
		sys[fmt.Sprintf("call %s %s %s %v", sd, sdPath, byNames, system.WatchedUnits)] = body
	}
	if body, err := bus.Call(true, sd, sdPath, fileState, ntp); err == nil {
		sys[fmt.Sprintf("call %s %s %s %s", sd, sdPath, fileState, ntp)] = body
	}
	units := "org.freedesktop.systemd1.Manager.ListUnitsFiltered"
	key := fmt.Sprintf("call org.freedesktop.systemd1 /org/freedesktop/systemd1 %s %v", units, []string{"failed"})
	if body, err := bus.Call(true, "org.freedesktop.systemd1", "/org/freedesktop/systemd1", units, []string{"failed"}); err == nil {
		sys[key] = body
	}
	if body, err := bus.Call(false, "org.freedesktop.systemd1", "/org/freedesktop/systemd1", units, []string{"failed"}); err == nil {
		ses[key] = body
	}
	// BlueZ solo se consulta si ya corre (NameHasOwner): pedirle sus objetos
	// sin dueño le pide a D-Bus que lo active. La respuesta se guarda con la
	// clave que lee bluetooth.running, para que la reproducción se comporte
	// igual que la máquina real.
	if c.btRunning(bus, sys) {
		if objs, err := bus.Objects(true, "org.bluez", "/"); err == nil {
			objs = filter(objs, map[string][]string{
				"org.bluez.Adapter1": {"Powered"},
				"org.bluez.Device1":  {"Name", "Alias", "Address", "Connected", "Paired", "Trusted"},
				"org.bluez.Battery1": {"Percentage"}})
			for _, ifs := range objs {
				if d := ifs["org.bluez.Device1"]; d != nil && c.anon != nil {
					for _, k := range []string{"Name", "Alias"} {
						if n, ok := d[k].(string); ok {
							d[k] = c.anon.Name(n)
						}
					}
				}
			}
			sys["objects org.bluez /"] = objs
		}
	}
	c.inhibitors(bus, sys)
	return map[string]map[string]any{"system": sys, "session": ses}
}

// btRunning pregunta a D-Bus si BlueZ corre (NameHasOwner, la misma llamada
// de bluetooth.running) sin activarlo, y guarda la respuesta en sys con la
// clave que lee ese paquete. Un error (p. ej. un bus sin esa respuesta) se
// trata como que no corre: nunca hay que activar BlueZ desde la captura.
func (c *capture) btRunning(bus source.Bus, sys map[string]any) bool {
	owner, err := bus.Call(true, "org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus.NameHasOwner", "org.bluez")
	if err != nil {
		return false
	}
	sys["call org.freedesktop.DBus /org/freedesktop/DBus org.freedesktop.DBus.NameHasOwner org.bluez"] = owner
	if len(owner) == 0 {
		return false
	}
	on, ok := owner[0].(bool)
	return ok && on
}

func filter(objs map[string]map[string]map[string]any, keep map[string][]string) map[string]map[string]map[string]any {
	out := map[string]map[string]map[string]any{}
	for p, ifs := range objs {
		m := map[string]map[string]any{}
		for iface, props := range ifs {
			ks, ok := keep[iface]
			if !ok {
				continue
			}
			pm := map[string]any{}
			for _, k := range ks {
				if v, ok := props[k]; ok {
					pm[k] = v
				}
			}
			m[iface] = pm
		}
		if len(m) > 0 {
			out[p] = m
		}
	}
	return out
}

// maintenance recrea lo que Mantenimiento mira por existencia o por enlace
// y mide las cachés: los kernels con su vmlinuz, las marcas de Omarchy, las
// raíces de las cachés (con sus tamaños en dirsizes.json) y el lock de
// Hyprland con adónde apunta su exe, en las rutas fijas de un fixture.
func (c *capture) maintenance() {
	vms, _ := c.env.FS.Glob("/usr/lib/modules/*/vmlinuz")
	for _, p := range vms {
		c.write("root"+p, nil)
	}
	marks, _ := c.env.FS.Glob(c.env.Home + "/.local/state/omarchy/re*-required")
	for _, p := range marks {
		c.write("root"+p, nil) // write reemplaza el home con --anonymize
	}
	sizes := map[string]maint.DirSize{}
	for _, r := range maint.CacheRoots(c.env) {
		c.write("root"+r.Path+"/.keep", nil)
		sizes[c.text(r.Path)] = maint.Walk(c.env.FS.Real(r.Path), maint.WalkMax)
	}
	if data, err := json.MarshalIndent(sizes, "", " "); err == nil {
		c.write(maint.SizesFile, data)
	}
	if pid := maint.HyprPID(c.env, c.env.HyprLock); pid != "" {
		if l, err := c.env.FS.Link("/proc/" + pid + "/exe"); err == nil {
			c.write("root"+source.FixtureHyprLock, []byte(pid+"\n"))
			c.write("root/proc/"+pid+"/exe.link", []byte(c.text(l)+"\n")) // puede vivir bajo el home
		}
	}
}

// pacmanAction es una línea de ALPM que nombra un paquete.
var pacmanAction = regexp.MustCompile(`^\[[^\]]+\] \[ALPM\] (upgraded|installed|removed|reinstalled|downgraded) (\S+)`)

// pacmanTS normaliza la fecha de una línea de pacman.log: el formato nuevo
// lleva la zona horaria del dueño de la máquina (p. ej. -0300), que clock()
// ya reemplaza por UTC en el resto de la captura; acá se reescribe igual,
// en vez de copiarla tal cual. El formato viejo (sin zona) no tiene nada
// que anonimizar y queda igual.
func pacmanTS(ts string) string {
	s := strings.TrimSuffix(strings.TrimPrefix(ts, "["), "]")
	if !strings.Contains(s, "T") {
		return ts
	}
	if t, ok := maint.ParseLogTime(s); ok {
		return "[" + t.Format("2006-01-02T15:04:05-0700") + "]"
	}
	return ts
}

// pacmanLog deja de pacman.log solo lo que lee Mantenimiento: la fecha de
// cada línea (en UTC, ver pacmanTS) y qué pasó, con los paquetes como pkgN
// (en orden de aparición), sin versiones, sin comandos (llevan rutas) y sin
// la salida de los scripts.
func (c *capture) pacmanLog(data []byte) []byte {
	var b strings.Builder
	names := map[string]string{}
	for _, l := range strings.Split(string(data), "\n") {
		end := strings.Index(l, "] ")
		if !strings.HasPrefix(l, "[") || end < 0 {
			continue
		}
		ts, msg := pacmanTS(l[:end+1]), l[end+2:]
		switch {
		case msg == "[PACMAN] starting full system upgrade", msg == "[ALPM] transaction completed":
			b.WriteString(ts + " " + msg + "\n")
		case strings.HasPrefix(msg, "[PACMAN] Running "):
			b.WriteString(ts + " [PACMAN] Running 'pacman'\n")
		default:
			if m := pacmanAction.FindStringSubmatch(l); m != nil {
				n, ok := names[m[2]]
				if !ok {
					n = fmt.Sprintf("pkg%d", len(names)+1)
					names[m[2]] = n
				}
				b.WriteString(ts + " [ALPM] " + m[1] + " " + n + "\n")
			}
		}
	}
	return []byte(b.String())
}

// pacmanConf deja de /etc/pacman.conf solo lo que lee Mantenimiento (el
// CacheDir de [options]; ver maint.PacmanCacheDir): nada de las demás
// claves ni secciones, que pueden traer un Server con credenciales de un
// mirror privado.
func pacmanConf(data []byte) []byte {
	var b strings.Builder
	section := ""
	for _, l := range strings.Split(string(data), "\n") {
		if i := strings.IndexByte(l, '#'); i >= 0 {
			l = l[:i]
		}
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = trimmed
			if section == "[options]" {
				b.WriteString(section + "\n")
			}
			continue
		}
		if section != "[options]" {
			continue
		}
		if k, _, ok := strings.Cut(trimmed, "="); ok && strings.TrimSpace(k) == "CacheDir" {
			b.WriteString(strings.TrimRight(l, " \t") + "\n")
		}
	}
	return []byte(b.String())
}

// snapperAllow es una línea ALLOW_USERS=… o ALLOW_GROUPS=… de un config de
// snapper, con o sin comillas (como las lee maint.shellVars).
var snapperAllow = regexp.MustCompile(`^(\s*)(ALLOW_USERS|ALLOW_GROUPS)(\s*=\s*)(["']?)([^"'\n]*?)(["']?)\s*$`)

// snapperConfig deja de un config de snapper (o del listado en
// /etc/conf.d/snapper) todo igual salvo ALLOW_USERS y ALLOW_GROUPS: son
// otros usuarios y grupos reales de la máquina. Quien corre la captura
// queda "user" (ya lo reemplaza Text, como en el resto del texto), así el
// permiso da lo mismo sobre el fixture; los demás quedan userN/groupN,
// estables en toda la captura. No toca comillas ni el resto del archivo.
func (c *capture) snapperConfig(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	for i, l := range lines {
		m := snapperAllow.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		indent, key, eq, q1, val, q2 := m[1], m[2], m[3], m[4], m[5], m[6]
		if q1 != q2 {
			continue // comillas rotas: se deja como está (shellVars tampoco la lee)
		}
		var names []string
		for _, f := range strings.Fields(val) {
			if t := c.anon.Text(f); t != f {
				names = append(names, t) // el que corre la captura, o el hostname
			} else if key == "ALLOW_USERS" {
				names = append(names, c.anon.User(f))
			} else {
				names = append(names, c.anon.Group(f))
			}
		}
		lines[i] = indent + key + eq + q1 + strings.Join(names, " ") + q2
	}
	return []byte(strings.Join(lines, "\n"))
}

// journalFields son los campos que pide el colector (--output-fields); el
// resto de cada línea (cursores, ids de arranque) no se guarda.
var journalFields = []string{"MESSAGE", "SYSLOG_IDENTIFIER", "_COMM", "PRIORITY", "__REALTIME_TIMESTAMP"}

// journal deja de cada línea de journalctl -o json solo los campos que se
// leen. Con anonymize: el mensaje de un error pasa a "message <N>"; el de
// una muerte por falta de memoria se reduce a lo que se lee (maint.OOMMessage)
// y su programa pasa a progN (ver oomProgram); SYSLOG_IDENTIFIER y _COMM
// (también nombres de programa) pasan a progN, salvo "kernel"; una línea de
// OOM que no es una muerte no se guarda.
func (c *capture) journal(args []string, out []byte) []byte {
	errs := slices.Equal(args, maint.JournalErrArgs)
	var b bytes.Buffer
	n := 0
	for _, l := range bytes.Split(out, []byte("\n")) {
		var m map[string]json.RawMessage
		if len(bytes.TrimSpace(l)) == 0 || json.Unmarshal(l, &m) != nil {
			continue
		}
		kept := map[string]json.RawMessage{}
		for _, k := range journalFields {
			if v, ok := m[k]; ok {
				kept[k] = v
			}
		}
		if c.anon != nil {
			for _, k := range []string{"SYSLOG_IDENTIFIER", "_COMM"} {
				if raw, ok := kept[k]; ok {
					var s string
					json.Unmarshal(raw, &s)
					kept[k], _ = json.Marshal(c.anon.Prog(s))
				}
			}
			var msg string
			json.Unmarshal(kept["MESSAGE"], &msg)
			if errs {
				n++
				msg = fmt.Sprintf("message %d", n)
			} else if reduced, ok := maint.OOMMessage(msg); ok {
				msg = c.oomProgram(reduced)
			} else {
				continue
			}
			kept["MESSAGE"], _ = json.Marshal(msg)
		}
		line, err := json.Marshal(kept)
		if err != nil {
			continue
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return b.Bytes()
}

// oomKernelProgram captura el programa entre paréntesis de un mensaje del
// kernel ya reducido (maint.OOMMessage): "Out of memory: Killed process
// <pid> (<programa>)".
var oomKernelProgram = regexp.MustCompile(`^(Out of memory: Killed process \d+ \()(.*)(\))$`)

// oomProgram pseudonimiza el programa de una muerte ya reducida
// (maint.OOMMessage): el nombre entre paréntesis del kernel, o la unidad de
// systemd-oomd (ver oomdUnit).
func (c *capture) oomProgram(msg string) string {
	if m := oomKernelProgram.FindStringSubmatch(msg); m != nil {
		return m[1] + c.anon.Prog(m[2]) + m[3]
	}
	if unit, ok := strings.CutPrefix(msg, "Killed "); ok {
		return "Killed " + c.oomdUnit(unit)
	}
	return msg
}

// genericSlice son los .slice que no identifican una app: la raíz, los de
// systemd para usuarios, servicios y máquinas, y el de un usuario por su
// uid (user-1000.slice). Un slice de plantilla (system-foo.slice,
// app-foo.slice) sí nombra algo, así que no está.
var genericSlice = regexp.MustCompile(`^(-|user|system|machine|user-[0-9]+)\.slice$`)

// oomdUnit reduce la unidad de una muerte de systemd-oomd (una ruta de
// cgroup): conserva solo los .slice genéricos del principio (genericSlice)
// y pseudonimiza como un solo progN todo lo que sigue desde el primer tramo
// que no lo es —la unidad, su .scope/.service, un slice de plantilla y
// cualquier tramo anidado después—, sin importar cuántos tramos tenga ni
// dónde, dentro de esa cola, viva el nombre de la app. Si no queda cola (el
// cgroup matado es él mismo genérico) la ruta queda igual.
func (c *capture) oomdUnit(unit string) string {
	segs := strings.Split(strings.TrimPrefix(unit, "/"), "/")
	i := 0
	for i < len(segs) && genericSlice.MatchString(segs[i]) {
		i++
	}
	parts := append([]string{}, segs[:i]...)
	if rest := strings.Join(segs[i:], "/"); rest != "" {
		parts = append(parts, c.anon.Prog(rest))
	}
	return "/" + strings.Join(parts, "/")
}

// coredumpFields son los campos de coredumpctl --json=short que se leen
// (gid no se lee: no está en model.Crash ni en el coredump de crashes.go).
var coredumpFields = []string{"time", "pid", "uid", "sig", "corefile", "exe", "size"}

// coredumps deja de cada volcado solo los campos que se leen. Con anonymize:
// el exe pasa por exeProgram (se reduce a "/" + progN, sin el resto del
// camino); una salida que no es un arreglo de volcados no se guarda (como
// journal descarta una línea que no entiende), para no dejar pasar algo sin
// revisar.
func (c *capture) coredumps(out []byte) []byte {
	var list []map[string]json.RawMessage
	if len(bytes.TrimSpace(out)) == 0 || json.Unmarshal(out, &list) != nil {
		if c.anon != nil {
			return []byte{}
		}
		return out
	}
	res := make([]map[string]json.RawMessage, 0, len(list))
	for _, d := range list {
		kept := map[string]json.RawMessage{}
		for _, k := range coredumpFields {
			if v, ok := d[k]; ok {
				kept[k] = v
			}
		}
		if c.anon != nil {
			if raw, ok := kept["exe"]; ok {
				var exe string
				json.Unmarshal(raw, &exe)
				kept["exe"], _ = json.Marshal(c.exeProgram(exe))
			}
		}
		res = append(res, kept)
	}
	b, err := json.Marshal(res)
	if err != nil {
		if c.anon != nil {
			return []byte{}
		}
		return out
	}
	return append(b, '\n')
}

// exeProgram reduce el exe de un volcado a su programa: coredumpctl solo
// lee el último tramo (path.Base, ver maint/crashes.go), así que el resto
// del camino no se guarda — puede nombrar un proyecto o vivir bajo el home
// de otro usuario, y ninguno de los dos se lee. Un exe vacío queda vacío,
// para que el replay muestre "Unknown" como sin anonimizar.
func (c *capture) exeProgram(exe string) string {
	if exe == "" {
		return ""
	}
	return "/" + c.anon.Prog(path.Base(exe))
}

// cryptUUID es el uuid de un dm cifrado: CRYPT-<TIPO>-<hex del UUID de LUKS>-<nombre>.
var cryptUUID = regexp.MustCompile(`^(CRYPT-[A-Za-z0-9]+-)([0-9a-fA-F]{32})(-)`)

// lvmUUID es el uuid de un dm de LVM: LVM-<id opaco de LVM, alfanumérico>,
// sin nombre al final (a diferencia de CRYPT-). Sin $ al final: el archivo
// real trae un salto de línea, y "." nunca lo cruza.
var lvmUUID = regexp.MustCompile(`^(LVM-)([0-9A-Za-z]+)`)

// idRun es una corrida de 16 o más caracteres alfanuméricos: el WWID de un
// dm multipath (mpath-<WWID>, part<N>-mpath-<WWID>), el uuid de una
// partición de un LUKS con particiones (part<N>-CRYPT-<TIPO>-<uuid>) o el
// id de un pool de stratis (stratis-<id>) son igual de identificatorios que
// el de un CRYPT-/LVM- (dmUUID los trata aparte, con su propio contador),
// pero no tienen un prefijo/sufijo fijo: se redactan por longitud, así los
// prefijos cortos (mpath-, part1-, CRYPT-LUKS2-, stratis-) sobreviven solos.
var idRun = regexp.MustCompile(`[0-9A-Za-z]{16,}`)

// registerOtherDMIDs busca, antes de leer ningún archivo, los ids largos de
// cada dm/uuid que no es CRYPT- ni LVM- y los registra como Serial: Text
// los reemplaza después, iguales y estables, en todo lo que la captura
// escriba (dm/name, lsblk, y cualquier otro archivo que nombre el
// dispositivo por su WWID/id), así devKey sigue encontrando el mismo
// dispositivo en el fixture anonimizado.
func (c *capture) registerOtherDMIDs() {
	paths, _ := c.env.FS.Glob("/sys/block/dm-*/dm/uuid")
	for _, p := range paths {
		uuid, err := c.env.FS.Read(p)
		if err != nil || cryptUUID.MatchString(uuid) || lvmUUID.MatchString(uuid) {
			continue
		}
		for _, id := range idRun.FindAllString(uuid, -1) {
			c.anon.Serial(id)
		}
	}
}

// dmUUID anonimiza /sys/block/dm-*/dm/uuid: mantiene el prefijo (CRYPT-<TIPO>-
// o LVM-) para que quien lea el fixture siga distinguiendo cifrado de LVM
// simple, pero reemplaza el id real (de LUKS o de LVM) por un contador
// estable (%032x de c.crypts), igual en todo el dm de esta captura.
func (c *capture) dmUUID(data []byte) []byte {
	s := c.anon.Text(string(data))
	switch {
	case cryptUUID.MatchString(s):
		c.crypts++
		return []byte(cryptUUID.ReplaceAllString(s, "${1}"+fmt.Sprintf("%032x", c.crypts)+"${3}"))
	case lvmUUID.MatchString(s):
		c.crypts++
		return []byte(lvmUUID.ReplaceAllString(s, "${1}"+fmt.Sprintf("%032x", c.crypts)))
	}
	return []byte(s)
}

// listenersOnly deja de /proc/net/{tcp,udp}[6] la cabecera y solo las
// líneas que Seguridad lee: TCP en LISTEN (estado 0A) o UDP sin conectar
// (07). Las conexiones establecidas dirían con quién habla la máquina.
func listenersOnly(data []byte, tcp bool) []byte {
	want := "07"
	if tcp {
		want = "0A"
	}
	var b strings.Builder
	for i, l := range strings.SplitAfter(string(data), "\n") {
		if f := strings.Fields(l); i == 0 || (len(f) > 3 && f[3] == want) {
			b.WriteString(l)
		}
	}
	return []byte(b.String())
}

// procNet anonimiza las direcciones local y remota de /proc/net/{tcp,udp}[6]
// (ver Anonymizer.NetHex); puertos, estados, uid e inodos quedan.
func (c *capture) procNet(data []byte) []byte {
	lines := strings.SplitAfter(string(data), "\n")
	for i, l := range lines {
		f := strings.Fields(l)
		if i == 0 || len(f) < 3 {
			continue
		}
		for _, col := range f[1:3] {
			h, port, ok := strings.Cut(col, ":")
			if !ok {
				continue
			}
			if r := c.anon.NetHex(h); r != h {
				l = strings.Replace(l, col, r+":"+port, 1)
			}
		}
		lines[i] = l
	}
	return []byte(strings.Join(lines, ""))
}

// ufwRules deja de user.rules solo las tuplas (lo único que se lee), con
// las direcciones de destino y origen anonimizadas (conservando alcance:
// multicast y link-local quedan como en Anonymizer.NetHex), un perfil de app
// propio (dst/src) pasado por Prog (puede nombrar algo que instaló el
// dueño) y los comentarios (texto del usuario) como "rule N". El resultado
// pasa entero por Text: la interfaz de la dirección ("in_<iface>") puede
// llevar una MAC o el hostname (p. ej. wg-<hostname>).
func (c *capture) ufwRules(data []byte) []byte {
	var b strings.Builder
	n := 0
	for _, l := range strings.Split(string(data), "\n") {
		rest, ok := strings.CutPrefix(l, "### tuple ### ")
		if !ok {
			continue
		}
		comment := ""
		if i := strings.Index(rest, " comment="); i >= 0 {
			n++
			rest, comment = rest[:i], " comment="+hex.EncodeToString([]byte(fmt.Sprintf("rule %d", n)))
		}
		f := strings.Split(rest, " ")
		for _, i := range []int{3, 5} { // destino y origen
			if i < len(f) {
				addr, bits, cidr := strings.Cut(f[i], "/")
				if ip := net.ParseIP(addr); ip != nil {
					if r := c.anon.scopedIP(ip); r != addr {
						f[i] = r
						if cidr {
							f[i] += "/" + bits
						}
					}
				}
			}
		}
		if len(f) == 9 { // perfiles de app (dst y src): pueden llevar un nombre propio
			for _, i := range []int{6, 7} {
				if f[i] != "-" {
					f[i] = c.anon.Prog(f[i])
				}
			}
		}
		b.WriteString("### tuple ### " + strings.Join(f, " ") + comment + "\n")
	}
	return []byte(c.anon.Text(b.String()))
}

// security guarda lo que Seguridad lee fuera de Globs: los slaves de cada
// dm (por existencia), /etc/passwd reducido a nombre:x:uid: de los dueños
// de un socket (siempre: el archivo entero nombra a todos los usuarios) y,
// para cada socket propio que escucha, el nombre del proceso y el fd que lo
// tiene abierto, en las rutas de un fixture.
func (c *capture) security() {
	slaves, _ := c.env.FS.Glob("/sys/block/dm-*/slaves/*")
	for _, s := range slaves {
		c.write("root"+s+"/.keep", nil)
	}
	socks := security.ReadSockets(c.env, &collect.Report{Lang: "en"})
	users := security.Users(c.env)
	var ids []uint32
	for _, s := range socks {
		if !slices.Contains(ids, s.UID) {
			ids = append(ids, s.UID)
		}
	}
	slices.Sort(ids)
	var pw strings.Builder
	for _, id := range ids {
		n, ok := users[id]
		if !ok {
			continue
		}
		// Con anonymize: quien corre la captura (identificado por uid, no
		// por si su nombre coincide con $USER/el hostname; solo si es una
		// cuenta humana: una captura como root no renombra a root) queda
		// "user";
		// otro humano de la máquina (otro uid >= 1000) no debe seguir con su
		// nombre real: pasa por User, estable en toda la captura (mismo
		// criterio que ALLOW_USERS de snapper). Las cuentas de sistema
		// (< 1000) quedan igual.
		if c.anon != nil {
			switch {
			case id >= 1000 && id == c.env.User.UID:
				n = "user"
			case id >= 1000:
				n = c.anon.User(n)
			}
		}
		fmt.Fprintf(&pw, "%s:x:%d:\n", n, id)
	}
	if pw.Len() > 0 {
		c.write("root/etc/passwd", []byte(c.text(pw.String())))
	}
	want := map[uint64]bool{}
	for _, i := range security.OwnInodes(socks, c.env.User.UID) {
		want[i] = true
	}
	found, _ := security.ScanFDs(c.env.FS, want, false)
	// En orden de inodo (no el orden del mapa, que Go no garantiza): el
	// primer programa distinto visto siempre da el mismo progN entre
	// capturas repetidas de la misma máquina.
	inos := make([]uint64, 0, len(found))
	for ino := range found {
		inos = append(inos, ino)
	}
	slices.Sort(inos)
	for _, ino := range inos {
		p := found[ino]
		dir := fmt.Sprintf("root/proc/%d", p.PID)
		comm := p.Comm
		if c.anon != nil {
			comm = c.anon.Prog(comm) // el nombre de un programa propio, nunca en claro
		}
		c.write(dir+"/comm", []byte(comm+"\n"))
		c.write(dir+"/fd/"+p.FD+".link", []byte(fmt.Sprintf("socket:[%d]\n", ino)))
	}
}

// hyprMonitor es lo que se guarda de cada monitor de j/monitors: los campos
// que lee system.Live (nombre, modo en uso, escala, VRR, formato, preset,
// apagado, espejo, modos) y los que lee apps (id, posición y el id numérico
// de los workspaces). Nunca serial, description ni nombres de workspace.
type hyprMonitor struct {
	ID              int      `json:"id"`
	Name            string   `json:"name"`
	X               int      `json:"x"`
	Y               int      `json:"y"`
	Width           int      `json:"width"`
	Height          int      `json:"height"`
	RefreshRate     float64  `json:"refreshRate"`
	Scale           float64  `json:"scale"`
	VRR             bool     `json:"vrr"`
	Format          string   `json:"currentFormat"`
	Preset          string   `json:"colorManagementPreset"`
	Disabled        bool     `json:"disabled"`
	MirrorOf        string   `json:"mirrorOf"`
	Modes           []string `json:"availableModes"`
	ActiveWorkspace struct {
		ID int `json:"id"`
	} `json:"activeWorkspace"`
	SpecialWorkspace struct {
		ID int `json:"id"`
	} `json:"specialWorkspace"`
}

// energy guarda lo que Energía y Dispositivos miran por enlace o por
// existencia y lo que piden a Hyprland: el port/peer de cada USB (como
// .link), el "mantener despierto" de Omarchy, j/monitors reducido a
// hyprMonitor y de j/clients solo las ventanas que impiden el reposo, con
// class, title, mapped e inhibitingIdle. Con anonymize la clase pasa por
// Prog y el título a "title <N>".
func (c *capture) energy() {
	usbs, _ := c.env.FS.Glob("/sys/bus/usb/devices/*")
	for _, d := range usbs {
		if l, err := c.env.FS.Link(d + "/port/peer"); err == nil {
			c.write("root"+d+"/port/peer.link", []byte(l+"\n"))
		}
	}
	if c.env.Home != "" && c.env.FS.Exists(c.env.Home+power.StayAwakeFile) {
		c.write("root"+c.env.Home+power.StayAwakeFile, nil) // write reemplaza el home con --anonymize
	}
	if c.env.Hypr == nil {
		return
	}
	if out, err := c.env.Hypr.Request("j/monitors"); err == nil {
		var ms []hyprMonitor
		if json.Unmarshal(out, &ms) == nil {
			if b, err := json.Marshal(ms); err == nil {
				c.write("hypr/"+source.Key("j/monitors")+".txt", b)
			}
		}
	}
	if out, err := c.env.Hypr.Request("j/clients"); err == nil {
		var cs []struct {
			Class          string `json:"class"`
			Title          string `json:"title"`
			Mapped         bool   `json:"mapped"`
			InhibitingIdle bool   `json:"inhibitingIdle"`
		}
		if json.Unmarshal(out, &cs) == nil {
			keep := cs[:0]
			for _, w := range cs {
				if !w.Mapped || !w.InhibitingIdle {
					continue
				}
				if c.anon != nil {
					w.Class = c.anon.Prog(w.Class)
					w.Title = c.numbered(c.titles, "title", w.Title)
				}
				keep = append(keep, w)
			}
			if b, err := json.Marshal(keep); err == nil {
				c.write("hypr/"+source.Key("j/clients")+".txt", b)
			}
		}
	}
}

// numbered da "<prefix> <N>" para s: el mismo texto, el mismo número (N
// cuenta desde 1 en cada mapa). El texto vacío no cambia.
func (c *capture) numbered(m map[string]int, prefix, s string) string {
	if s == "" {
		return s
	}
	n, ok := m[s]
	if !ok {
		n = len(m) + 1
		m[s] = n
	}
	return fmt.Sprintf("%s %d", prefix, n)
}

// inhibitors guarda lo que lee power.Inhibit de logind: NameHasOwner y, si
// corre, ListInhibitors, con las claves del FakeBus. Con anonymize, quién
// pasa por Prog (solo "kernel" queda) y el motivo a "reason <N>": los escribe
// cada programa y pueden nombrar lo que se está mirando. Qué, modo, uid y pid
// se conservan.
func (c *capture) inhibitors(bus source.Bus, sys map[string]any) {
	owner, err := bus.Call(true, "org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus.NameHasOwner", power.Login1)
	if err != nil {
		return
	}
	sys["call org.freedesktop.DBus /org/freedesktop/DBus org.freedesktop.DBus.NameHasOwner "+power.Login1] = owner
	if len(owner) == 0 {
		return
	}
	if on, ok := owner[0].(bool); !ok || !on {
		return
	}
	body, err := bus.Call(true, power.Login1, power.Login1Path, power.ListInhibitors)
	if err != nil {
		return
	}
	// Pasa por JSON: el bus real da structs y el FakeBus []any; así se
	// recorre y se guarda igual.
	var norm []any
	if b, err := json.Marshal(body); err != nil || json.Unmarshal(b, &norm) != nil {
		return
	}
	if c.anon != nil && len(norm) > 0 {
		if list, ok := norm[0].([]any); ok {
			for _, it := range list {
				f, ok := it.([]any)
				if !ok || len(f) < 3 {
					continue
				}
				if who, ok := f[1].(string); ok {
					f[1] = c.anon.Prog(who)
				}
				if why, ok := f[2].(string); ok {
					f[2] = c.numbered(c.reasons, "reason", why)
				}
			}
		}
	}
	sys["call "+power.Login1+" "+power.Login1Path+" "+power.ListInhibitors] = norm
}
