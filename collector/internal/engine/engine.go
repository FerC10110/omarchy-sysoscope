// Package engine orquesta los colectores con ritmos por fuente: lo barato en
// cada muestra, lo que crea procesos o consulta D-Bus cada varias, el
// inventario una vez por arranque (en caché) y lo caro solo a pedido.
package engine

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/apps"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/audio"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/bluetooth"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/cpu"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/gpu"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/history"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/maint"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/mem"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/network"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/power"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/security"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/sensors"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/storage"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/system"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/wifi"
)

// Options configura el Engine. Stream indica un proceso de larga vida
// (habilita nvidia-smi en bucle y las lecturas en gorutinas). Statfs y
// DirSize son para tests (nil = unix.Statfs y maint.Walk); CheckupDB es la
// base de checkupdates que lee pacman -Sddp ("" = la de checkupdates).
type Options struct {
	Interval  time.Duration
	Focus     string
	Stream    bool
	Statfs    func(path string) (size, free, avail uint64, err error)
	DirSize   maint.DirSizeFunc
	CheckupDB string
}

// Ritmos, en muestras (ticks) o en tiempo.
const (
	everySensors = 3 // sensores y nvidia-smi
	everyNet     = 5 // direcciones, gateway, Wi-Fi y uso de filesystems
	slowPeriod   = 60 * time.Second
	slowChips    = 5 * time.Minute // chips hwmon lentos (WMI de ASUS) fuera de la sección Sensores
	saveEvery    = 60 * time.Second
	recentSpan   = 2 * time.Minute
	wifiEvery    = 10 * time.Second // redes Wi-Fi (nmcli), solo con la vista Red
	cachesEvery  = 10 * time.Minute // tamaño de las cachés, solo con Mantenimiento abierta
	healthEvery  = 30 * time.Second // errores del journal, crashes y OOM: solo con Mantenimiento (los OOM, también Memoria) abierta
	oomKillEvery = 5 * time.Second  // una muerte nueva (oom_kill cambió) se lee enseguida, pero a lo sumo con este intervalo
)

// Engine produce un Snapshot por Tick. El Snapshot devuelto comparte
// memoria con el Engine: es válido hasta el Tick siguiente.
type Engine struct {
	env  source.Env
	opts Options
	tick int

	cpu     *model.CPU
	modules *model.Memory
	gpus    []model.GPU
	sys     *model.System
	storage *model.Storage
	net     *model.Network
	sensors *model.Sensors
	power   *model.Power
	fw      *model.Firmware
	privacy *model.Privacy

	cpuS     cpu.Sampler
	sensR    sensors.Reader
	gpuS     gpu.Sampler
	io       storage.IO
	netS     network.Sampler
	powerS   power.Sampler
	appsS    apps.Sampler
	appsV    *model.Apps
	appsDue  bool // se entró a una vista que muestra las apps: leerlas ya
	appsAt   time.Time
	appsIdle model.Apps
	frz      *apps.Freezer

	audioR   audio.Reader
	audioDue bool // una acción audio.* cambió algo: leerlo en la próxima muestra

	// unavailable por grupo de ritmo; cada grupo reemplaza su lista al correr
	unStatic, unFast, unSensors, unNet, unSlow, unFw, unApps, unAudio, unBT []model.Unavailable

	lastSlow   time.Time
	forceSlow  bool
	powerDue   bool                // una acción cambió el perfil: releerlo en el próximo tick
	unInhibit  []model.Unavailable // inhibidores y ventanas (solo con Energía abierta)
	liveDue    bool                // se entró a Dispositivos: releer el modo en uso de los monitores ya
	btDue      bool                // una acción bt.* cambió algo: releer Bluetooth en el próximo tick
	pacmanSeen int

	// fwupd corre en su goroutine; Tick recoge el resultado.
	fwMu      sync.Mutex
	fwRunning bool
	fwResult  *fwResult

	// Wi-Fi: nmcli corre solo con la vista Red abierta, en su gorutina en el
	// stream y en línea en un comando de una sola muestra.
	wifiR       wifi.Reader
	wifiMu      sync.Mutex
	wifiRunning bool
	wifiRes     *wifi.Scan // lectura terminada, sin aplicar todavía
	wifiScan    *wifi.Scan // la que se muestra
	wifiAt      time.Time  // cuándo empezó la última lectura (cero: leer ya)
	actLogSeen  bool       // ya se miró actions.log al menos una vez
	actLogMod   time.Time  // su mtime la última vez que se miró
	actLogSize  int64      // su tamaño la última vez que se miró

	// Mantenimiento: reinicio y pacman.log con lo lento (también con la
	// sección cerrada: el Resumen avisa con ellos); cachés y snapshots solo
	// con la sección abierta (maintTick); checkupdates solo a pedido.
	maintLog     maint.LogReader
	reboot       *model.Reboot
	upgrade      *model.Upgrade
	updates      *model.Updates // última comprobación (CheckUpdates o updates.json)
	updMod       time.Time      // mtime de updates.json la última vez que se leyó
	updMu        sync.Mutex
	updRunning   bool
	updRes       *model.Updates // comprobación terminada, sin aplicar todavía
	maintMu      sync.Mutex
	maintRunning bool
	maintRes     *maintResult // lectura terminada, sin aplicar todavía
	cachesDue    bool         // refresh o cambio de idioma: medir ya
	snapsDue     bool         // se entró a la sección o refresh: leer ya
	snapsRead    bool
	caches       []model.CacheDir
	cachesAt     time.Time
	snaps        *model.Snapshots
	unCaches     []model.Unavailable
	unSnaps      []model.Unavailable

	// Salud: errores, crashes y OOM con Mantenimiento abierta (los OOM
	// también con Memoria), cada healthEvery, en maintTick; se conserva lo
	// leído al salir.
	health    *model.Health
	healthAt  time.Time // última lectura completa
	oomAt     time.Time // última lectura de los OOM (completa o sola)
	healthDue bool      // refresh o cambio de idioma: releer la salud ya
	oomDue    bool      // lo mismo para los OOM
	oomSeen   uint64    // oom_kill en la última lectura de los OOM
	oomKills  *uint64   // oom_kill de la muestra en curso
	unHealth  []model.Unavailable

	// Seguridad (securityTick): puertos, firewall y cifrado con lo lento;
	// el programa de los sockets propios solo con la sección abierta.
	units    system.Units // de system.Slow: la misma llamada que el reloj
	secSocks []security.Socket
	secUsers map[uint32]string
	fwR      security.FirewallReader
	secFw    *model.Firewall
	secVols  []model.CryptVolume
	secSwaps []model.SwapCrypt
	owners   security.Owners
	unSec    []model.Unavailable
	// Snapshot.Security armado (securityState) y lo que se usó para armarlo:
	// se reusa mientras nada de eso cambie.
	secState  *model.Security
	secDirty  bool          // cambió algo leído en securityTick
	secOpen   bool          // con la sección abierta (con programas)
	secIfaces []model.Iface // las interfaces de ese momento
	secBuilds int           // veces que se armó (tests)

	store    *history.Store
	lastSave time.Time
	recent   map[string][]float64
	self     selfMeter
}

// New recolecta el inventario (o lo toma de la caché) y abre el historial.
func New(env source.Env, opts Options) *Engine {
	e := &Engine{env: env, opts: opts, recent: map[string][]float64{}}
	e.sensR.SlowEvery = slowChips
	e.SetInterval(opts.Interval)
	if e.opts.Statfs == nil {
		e.opts.Statfs = statfs
	}
	if e.opts.DirSize == nil {
		root := env.FS
		e.opts.DirSize = func(p string) maint.DirSize { return maint.Walk(root.Real(p), maint.WalkMax) }
	}
	e.appsS.UID = uint32(os.Getuid())
	e.appsS.Self = apps.SelfChain(env.FS, os.Getpid())
	e.frz = apps.LoadFreezer(env)
	e.SetFocus(opts.Focus)
	e.static()
	var err error
	e.store, err = history.Load(filepath.Join(env.State, "history.v1"))
	if errors.Is(err, history.ErrCorrupt) {
		e.unStatic = append(e.unStatic, model.Unavailable{What: "history", Reason: "error",
			Message: i18n.T(env.Lang, "the history file was damaged; it was set aside and a new one was started")})
	}
	e.lastSave = env.Now()
	return e
}

type fwResult struct {
	fw *model.Firmware
	un []model.Unavailable
}

func (e *Engine) static() {
	boot := e.bootID()
	if c := e.loadCache(boot); c != nil {
		e.cpu, e.modules, e.gpus, e.sys, e.unStatic = c.CPU, c.Modules, c.GPUs, c.System, c.Unavailable
		// El caché no guarda el hostname ni las series de los monitores.
		system.Identity(e.env, e.sys)
		e.pacmanSeen = e.sys.Packages.Pacman // Foreign ya está en el caché
		return
	}
	r := &collect.Report{Lang: e.env.Lang}
	e.cpu = cpu.Static(e.env, r)
	e.modules = &model.Memory{}
	mem.Modules(e.env, r, e.modules)
	e.gpus = gpu.Static(e.env, r)
	e.sys = system.Static(e.env, r)
	system.Foreign(e.env, e.sys, r)
	e.pacmanSeen = e.sys.Packages.Pacman
	e.unStatic = r.List
	e.saveCache(&staticCache{Schema: model.Schema, BootID: boot, Lang: e.env.Lang, CPU: e.cpu, Modules: e.modules,
		GPUs: e.gpus, System: e.sys, Unavailable: r.List})
}

func statfs(path string) (uint64, uint64, uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, 0, 0, err
	}
	bs := uint64(st.Bsize)
	return st.Blocks * bs, st.Bfree * bs, st.Bavail * bs, nil
}

// showsApps son las vistas que muestran las apps: Aplicaciones y las tres
// que más usan al final de CPU, Memoria, GPU y Red.
var showsApps = map[string]bool{"apps": true, "cpu": true, "memory": true, "gpu": true, "network": true}

// SetFocus indica la sección abierta: "gpu" pide los procesos por GPU,
// "sensors" lee también los chips
// lentos en cada lectura y "security" mira quién usa la cámara y el
// micrófono. El firmware se pide aparte, con CheckFirmware.
// "power" relee el perfil de energía y lee los inhibidores en cada muestra.
// "devices" relee además el modo en uso de los monitores al entrar y cada
// everyNet muestras.
// "devices" lee el audio (pactl) y Bluetooth en cada muestra.
// "apps" lee además el disco de cada proceso (/proc/<pid>/io); al salir de
// ella se olvida su base, para que la primera lectura al volver no promedie
// el rato afuera.
// "maintenance" mide las cachés y lee los snapshots (maintTick).
// "maintenance" lee también la salud y "memory" los procesos matados por
// falta de memoria (maintTick).
func (e *Engine) SetFocus(section string) {
	section = strings.TrimSpace(section)
	if showsApps[section] && section != e.opts.Focus {
		e.appsDue = true
	}
	if section == "devices" && e.opts.Focus != "devices" {
		e.liveDue = true
	}
	if section == "maintenance" && e.opts.Focus != "maintenance" {
		e.maintMu.Lock()
		e.snapsDue = true
		e.maintMu.Unlock()
	}
	if e.opts.Focus == "apps" && section != "apps" {
		e.appsS.DropDisk()
	}
	e.opts.Focus = section
	e.appsS.Disk = section == "apps"
	e.gpuS.Procs = e.opts.Focus == "gpu"
	e.sensR.Focus = e.opts.Focus == "sensors"
	if e.opts.Focus != "security" {
		e.privacy = nil
	}
}

// CheckFirmware consulta fwupd en segundo plano (puede tardar y sale a
// internet). Una llamada mientras otra corre no hace nada; el primer Tick
// después de que termina trae el resultado.
func (e *Engine) CheckFirmware() {
	e.fwMu.Lock()
	defer e.fwMu.Unlock()
	if e.fwRunning {
		return
	}
	e.fwRunning = true
	env := e.env
	go func() {
		r := &collect.Report{Lang: env.Lang}
		res := &fwResult{fw: system.Firmware(env, r), un: r.List}
		e.fwMu.Lock()
		e.fwResult, e.fwRunning = res, false
		e.fwMu.Unlock()
	}()
}

// FirmwareRunning dice si hay una consulta a fwupd en curso.
func (e *Engine) FirmwareRunning() bool {
	e.fwMu.Lock()
	defer e.fwMu.Unlock()
	return e.fwRunning
}

// RefreshWifi hace que la próxima muestra vuelva a leer el Wi-Fi (una
// acción lo cambió). Sin la vista Red no hace nada.
func (e *Engine) RefreshWifi() {
	e.wifiMu.Lock()
	e.wifiAt = time.Time{}
	e.wifiMu.Unlock()
}

// wifiTick lee el Wi-Fi cada wifiEvery con la vista Red abierta (nunca dos
// lecturas a la vez) y copia la última a las placas. Fuera de esa vista no
// corre nmcli y borra lo leído.
func (e *Engine) wifiTick(now time.Time) {
	if e.opts.Focus != "network" {
		e.wifiMu.Lock()
		e.wifiScan, e.wifiRes, e.wifiAt = nil, nil, time.Time{}
		e.wifiMu.Unlock()
		wifi.Apply(e.net, nil)
		return
	}
	var names []string
	if e.net != nil {
		for _, it := range e.net.Interfaces {
			if it.Kind == "wifi" {
				names = append(names, it.Name)
			}
		}
	}
	e.wifiMu.Lock()
	if e.wifiRes != nil {
		e.wifiScan, e.wifiRes = e.wifiRes, nil
	}
	// Una acción de otro proceso (lazylazy, o un wifi-wait desacoplado que
	// terminó una conexión o un cambio de banda) deja su rastro en
	// actions.log: un stat por muestra (barato) alcanza para notarlo y no
	// esperar los 10 s hasta la próxima lectura.
	if fi, err := os.Stat(filepath.Join(e.env.State, "actions.log")); err == nil {
		if e.actLogSeen && (!fi.ModTime().Equal(e.actLogMod) || fi.Size() != e.actLogSize) {
			e.wifiAt = time.Time{}
		}
		e.actLogSeen, e.actLogMod, e.actLogSize = true, fi.ModTime(), fi.Size()
	}
	start := len(names) > 0 && !e.wifiRunning && (e.wifiAt.IsZero() || now.Sub(e.wifiAt) >= wifiEvery)
	if start {
		e.wifiRunning, e.wifiAt = true, now
	}
	e.wifiMu.Unlock()
	if start {
		if e.opts.Stream {
			env := e.env
			go func() {
				sc := e.wifiR.Read(env, names, now)
				e.wifiMu.Lock()
				e.wifiRes, e.wifiRunning = sc, false
				e.wifiMu.Unlock()
			}()
		} else {
			sc := e.wifiR.Read(e.env, names, now)
			e.wifiMu.Lock()
			e.wifiScan, e.wifiRunning = sc, false
			e.wifiMu.Unlock()
		}
	}
	wifi.Apply(e.net, e.wifiScan)
}

// SetLang cambia el idioma de los mensajes. Como vienen traducidos de la
// lectura, se vuelve a leer todo en el próximo tick (el inventario también:
// el caché guarda su idioma).
func (e *Engine) SetLang(lang string) {
	if lang == e.env.Lang {
		return
	}
	e.env.Lang = lang
	e.unSlow, e.unSensors, e.unNet, e.unFw, e.unBT = nil, nil, nil, nil, nil
	e.maintMu.Lock()
	e.unCaches, e.unSnaps, e.cachesDue, e.snapsDue = nil, nil, true, true
	e.unHealth, e.healthDue, e.oomDue = nil, true, true
	e.maintMu.Unlock()
	e.static()
	e.RefreshSlow()
}

// SetInterval fija el intervalo entre muestras, entre 1 y 60 s.
func (e *Engine) SetInterval(d time.Duration) {
	e.opts.Interval = min(max(d, time.Second), 60*time.Second)
	if e.opts.Stream {
		e.gpuS.Loop = everySensors * e.opts.Interval
	}
}

func (e *Engine) Interval() time.Duration { return e.opts.Interval }

// RefreshSlow hace que el próximo tick corra todo lo lento.
func (e *Engine) RefreshSlow() {
	e.forceSlow = true
	e.tick = 0
}

// RefreshPower hace que la próxima muestra vuelva a leer el perfil de energía
// (una acción power.* lo cambió).
func (e *Engine) RefreshPower() { e.powerDue = true }

// RefreshAudio hace que la próxima muestra lea el audio completo (una
// acción audio.* lo cambió): sin esperar tras una falla y aunque la vista
// Dispositivos no esté abierta. resetAvail, además, hace que esa lectura
// vuelva a correr el script de disponibilidad de Omarchy en vez de reusar
// lo que tenía en caché: solo audio.default lo necesita (las demás
// acciones no la cambian, y correrlo en cada tecla es lo que se está
// arreglando).
func (e *Engine) RefreshAudio(resetAvail bool) {
	if resetAvail {
		e.audioR.Reset()
	}
	e.audioDue = true
}

// RefreshBluetooth hace que la próxima muestra relea Bluetooth (una acción
// bt.* lo cambió).
func (e *Engine) RefreshBluetooth() { e.btDue = true }

// Tick toma una muestra completa según los ritmos.
func (e *Engine) Tick() *model.Snapshot {
	start := cpuTime()
	env, now := e.env, e.env.Now()
	slow := e.forceSlow || e.lastSlow.IsZero() || now.Sub(e.lastSlow) >= slowPeriod
	sens := e.tick%everySensors == 0
	netSlow := e.tick%everyNet == 0 || slow

	if slow {
		r := &collect.Report{Lang: env.Lang}
		e.storage = storage.Static(env, r)
		storage.Smart(env, e.storage.Disks, r)
		if e.net == nil {
			e.net = &model.Network{}
		}
		e.net.Interfaces = network.Static(env, r)
		e.units = system.Slow(env, e.sys, r)
		if e.sys.Packages.Pacman != e.pacmanSeen {
			system.Foreign(env, e.sys, r)
			e.pacmanSeen = e.sys.Packages.Pacman
		}
		e.reboot = maint.ReadReboot(env, e.sys.Kernel, env.HyprLock)
		e.upgrade = e.maintLog.Read(env, r)
		if e.power == nil {
			e.power = &model.Power{}
		}
		power.Sleep(env, e.power) // modo de suspensión y qué despierta el equipo
		e.unSlow = r.List
	}
	// El modo en uso de los monitores (j/monitors por el socket): con lo
	// lento, después de releer el inventario, en la muestra siguiente a
	// entrar a Dispositivos y cada everyNet muestras con ella abierta.
	if slow || e.liveDue || (e.opts.Focus == "devices" && netSlow) {
		system.Live(env, e.sys.Monitors)
	}
	e.liveDue = false
	// Bluetooth (D-Bus y /sys, barato): con lo lento, en cada muestra con la
	// vista Dispositivos abierta y en la siguiente a una acción bt.*.
	if slow || e.btDue || e.opts.Focus == "devices" {
		rb := &collect.Report{Lang: env.Lang}
		bluetooth.Sample(env, e.sys, rb)
		e.unBT = rb.List
	}
	e.btDue = false
	if sens {
		r := &collect.Report{Lang: env.Lang}
		e.sensors = e.sensR.Read(env, r)
		e.unSensors = r.List
	}

	rf := &collect.Report{Lang: env.Lang}
	// Las frecuencias (24 archivos) van con los sensores, también con la vista
	// CPU abierta: solo muestra la media, y leerlas en cada tick costaba ~1.5 ms.
	e.cpuS.SkipFreq = !sens
	e.cpuS.Sample(env, e.cpu, e.sensors, rf)
	m := mem.Read(env, rf)
	e.oomKills = m.OOMKills
	m.Slots, m.CapacityB, m.ECC, m.Modules = e.modules.Slots, e.modules.CapacityB, e.modules.ECC, e.modules.Modules
	e.gpuS.Sample(env, e.gpus, rf, sens)
	e.io.Sample(env, e.storage.Disks)
	diskTemps(e.storage.Disks, e.sensors)
	// El perfil (D-Bus y los archivos de Omarchy) va con lo lento, en cada
	// muestra con la vista Energía abierta y en la siguiente a una acción.
	e.powerS.Sample(env, e.power, slow || e.powerDue || e.opts.Focus == "power", rf)
	e.powerDue = false
	// Qué impide suspender o apagar la pantalla: solo con Energía abierta,
	// en cada muestra (una llamada D-Bus, un pedido al socket y un stat).
	// Fuera de ella no se muestra nada viejo.
	if e.opts.Focus == "power" {
		ri := &collect.Report{Lang: env.Lang}
		e.power.Inhibit = power.Inhibit(env, ri)
		e.unInhibit = ri.List
	} else {
		e.power.Inhibit, e.unInhibit = nil, nil
	}
	e.unFast = rf.List

	rn := &collect.Report{Lang: env.Lang}
	e.netS.Sample(env, e.net, netSlow, rn)
	if netSlow {
		storage.Usage(e.storage.Filesystems, e.opts.Statfs)
		e.unNet = rn.List
	}
	e.wifiTick(now)
	e.maintTick(now)
	// updates.json con lo lento y, con la sección abierta, en cada muestra
	// (un stat): así se ve enseguida una comprobación de otro proceso.
	if slow || e.opts.Focus == "maintenance" {
		e.loadUpdates()
	}
	// El audio (tres pactl) solo con la vista Dispositivos abierta, en cada
	// muestra, y en la siguiente a una acción audio.*. Fuera de ella no corre
	// nada y no se muestra.
	if e.opts.Focus == "devices" || e.audioDue {
		ra := &collect.Report{Lang: env.Lang}
		e.sys.Sound = e.audioR.Read(context.Background(), env, ra)
		e.unAudio = ra.List
	} else {
		e.sys.Sound, e.unAudio = nil, nil
	}
	e.audioDue = false
	// Las aplicaciones (recorrer /proc, Hyprland, ss y la GPU) solo con una
	// vista que las muestra: al entrar, y después cada 2 muestras en
	// Aplicaciones y cada 5 en CPU, Memoria, GPU y Red. Leerlas siempre
	// costaba ~3 ms por muestra con el panel cerrado.
	if !showsApps[e.opts.Focus] {
		e.appsV = nil
	} else if e.appsDue || (e.opts.Focus == "apps" && e.tick%2 == 0) || netSlow {
		// Una base más vieja que dos vueltas del ritmo lento daría tasas
		// promediadas sobre todo ese rato: esa lectura cuenta como la
		// primera (sin tasas) y se vuelve a leer en la muestra siguiente.
		stale := e.appsAt.IsZero() || now.Sub(e.appsAt) > 2*everyNet*e.opts.Interval
		if stale {
			e.appsS.Reset()
		}
		ra := &collect.Report{Lang: env.Lang}
		threads := 0
		if e.cpu != nil {
			threads = e.cpu.Threads
		}
		e.appsV = e.appsS.Sample(env, e.gpus, threads, netSlow || e.appsDue || stale, ra)
		e.unApps = ra.List
		e.appsDue, e.appsAt = stale, now
	}
	if e.appsV == nil {
		e.unApps = nil
	}
	// Lo congelado vence aunque nadie mire las apps, y la barra lo muestra:
	// sale del freezer, no de recorrer /proc.
	e.frz.ResumeDue(env, now)
	av := e.appsV
	if av == nil {
		e.appsIdle = model.Apps{List: []model.App{}}
		av = &e.appsIdle
	}
	for i := range av.List {
		av.List[i].Frozen = e.frz.Frozen(av.List[i].ID)
	}
	av.Frozen = e.frz.List()

	e.fwMu.Lock()
	if res := e.fwResult; res != nil {
		e.fw, e.unFw, e.fwResult = res.fw, res.un, nil
	}
	e.fwMu.Unlock()
	e.securityTick(m, slow, netSlow)
	if slow {
		e.lastSlow, e.forceSlow = now, false
	}
	e.tick++

	snap := &model.Snapshot{Schema: model.Schema, Lang: env.Lang, Time: now.UTC(), CPU: e.cpu, Memory: m, GPUs: e.gpus,
		Storage: e.storage, Network: e.net, Sensors: e.sensors, Power: e.power, System: e.sys, Firmware: e.fw, Privacy: e.privacy, Apps: av,
		Maintenance: e.maintenance(), Security: e.securityState()}
	snap.Unavailable = merge(e.unStatic, e.unSlow, e.unBT, e.unSensors, e.unFast, e.unNet, e.unFw, e.unApps, e.unAudio, e.unCaches, e.unSnaps, e.unHealth, e.unSec, e.unInhibit)
	e.record(snap, now)
	tick, avg := e.self.measure(start)
	snap.Self = &model.Self{TickMs: round2(tick), AvgMs: round2(avg), RSSB: rss(), Interval: e.opts.Interval.Seconds()}
	return snap
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// merge une las listas sin repetir What.
func merge(lists ...[]model.Unavailable) []model.Unavailable {
	seen := map[string]bool{}
	var out []model.Unavailable
	for _, l := range lists {
		for _, u := range l {
			if !seen[u.What] {
				seen[u.What] = true
				out = append(out, u)
			}
		}
	}
	return out
}

// diskTemps toma la temperatura de cada disco NVMe de hwmon (más fresca) y,
// si no hay, la de SMART.
func diskTemps(disks []model.Disk, s *model.Sensors) {
	for i := range disks {
		d := &disks[i]
		d.TempC = nil
		if s != nil {
			for _, c := range s.Chips {
				if c.Kind == "nvme" && c.Label == "NVMe "+d.Model && len(c.Temps) > 0 {
					v := c.Temps[0].Value
					d.TempC = &v
					break
				}
			}
		}
		if d.TempC == nil && d.Smart != nil && d.Smart.TempC != nil {
			v := *d.Smart.TempC
			d.TempC = &v
		}
	}
}

// record suma la muestra al historial y al anillo de 2 minutos, y guarda el
// historial cada 60 s.
func (e *Engine) record(s *model.Snapshot, now time.Time) {
	vals := history.Values(s)
	e.store.Add(now, vals)
	keep := int(recentSpan / e.opts.Interval)
	for k, v := range vals {
		r := append(e.recent[k], v)
		if len(r) > keep {
			r = r[len(r)-keep:]
		}
		e.recent[k] = r
	}
	if now.Sub(e.lastSave) >= saveEvery {
		e.store.Save(filepath.Join(e.env.State, "history.v1"))
		e.lastSave = now
	}
}

// Recent devuelve los valores de los últimos 2 minutos de una métrica.
func (e *Engine) Recent(metric string) []float64 {
	return append([]float64(nil), e.recent[metric]...)
}

// History da acceso al historial (para exportar).
func (e *Engine) History() *history.Store { return e.store }

// Close termina los procesos auxiliares y guarda el historial.
func (e *Engine) Close() error {
	e.gpuS.Close()
	return e.store.Save(filepath.Join(e.env.State, "history.v1"))
}
