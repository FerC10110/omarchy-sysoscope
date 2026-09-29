// Package model es el esquema JSON de sysoscope (schema 1). Lo consumen el
// panel QML (etapa B) y lazylazy (etapa C): cambiarlo es un cambio de schema.
//
// Unidades: bytes, bytes/s, °C, W, MHz, porcentaje 0–100 y segundos. Un dato
// que no se pudo leer se omite (punteros con omitempty) y se explica en
// Snapshot.Unavailable; nunca se reporta como 0.
package model

import "time"

const Schema = 1

type Snapshot struct {
	Schema      int           `json:"schema"`
	Lang        string        `json:"lang"` // idioma de los mensajes: en o es
	Time        time.Time     `json:"time"`
	CPU         *CPU          `json:"cpu,omitempty"`
	Memory      *Memory       `json:"memory,omitempty"`
	GPUs        []GPU         `json:"gpus,omitempty"`
	Storage     *Storage      `json:"storage,omitempty"`
	Network     *Network      `json:"network,omitempty"`
	Sensors     *Sensors      `json:"sensors,omitempty"`
	Power       *Power        `json:"power,omitempty"`
	System      *System       `json:"system,omitempty"`
	Firmware    *Firmware     `json:"firmware,omitempty"`
	Privacy     *Privacy      `json:"privacy,omitempty"` // solo con la sección Seguridad abierta
	Apps        *Apps         `json:"apps,omitempty"`
	Maintenance *Maintenance  `json:"maintenance,omitempty"`
	Security    *Security     `json:"security,omitempty"` // puertos, firewall y cifrado (la cámara y el micrófono siguen en Privacy)
	Unavailable []Unavailable `json:"unavailable,omitempty"`
	Self        *Self         `json:"self,omitempty"`
}

// Unavailable explica un dato que no se pudo leer. What usa la ruta del campo
// ("gpu.power_w", "power.rapl", "memory.modules").
type Unavailable struct {
	What    string `json:"what"`
	Reason  string `json:"reason"` // missing_tool|permission|not_present|timeout|error
	Message string `json:"message"`
}

type PSI struct {
	Some10 float64  `json:"some10"`
	Some60 float64  `json:"some60"`
	Full10 *float64 `json:"full10,omitempty"`
	Full60 *float64 `json:"full60,omitempty"`
}

type CPU struct {
	Model           string          `json:"model"`
	Vendor          string          `json:"vendor"` // AMD|Intel|ARM|…
	Arch            string          `json:"arch"`
	Sockets         int             `json:"sockets"`
	Cores           int             `json:"cores"`
	Threads         int             `json:"threads"`
	MinMHz          *float64        `json:"min_mhz,omitempty"`
	MaxMHz          *float64        `json:"max_mhz,omitempty"`
	BaseMHz         *float64        `json:"base_mhz,omitempty"`
	Caches          []Cache         `json:"caches,omitempty"`
	Microcode       string          `json:"microcode,omitempty"`
	Governor        string          `json:"governor,omitempty"`
	Driver          string          `json:"driver,omitempty"`
	EPP             string          `json:"epp,omitempty"`
	Boost           *bool           `json:"boost,omitempty"`
	Capabilities    []Capability    `json:"capabilities,omitempty"`
	Vulnerabilities []Vulnerability `json:"vulnerabilities,omitempty"`
	Topology        []LogicalCPU    `json:"topology,omitempty"`
	// dinámico
	UsagePct   *float64   `json:"usage_pct,omitempty"`
	PerCPUPct  []float64  `json:"per_cpu_pct,omitempty"`
	FreqMHz    []float64  `json:"freq_mhz,omitempty"`
	AvgMHz     *float64   `json:"avg_mhz,omitempty"`
	TempC      *float64   `json:"temp_c,omitempty"`
	TempSensor string     `json:"temp_sensor,omitempty"` // "k10temp Tdie"
	Load       [3]float64 `json:"load"`
	UptimeS    float64    `json:"uptime_s"`
	PSI        *PSI       `json:"psi,omitempty"`
}

type Cache struct {
	Level     int    `json:"level"`
	Type      string `json:"type"` // Data|Instruction|Unified
	SizeB     uint64 `json:"size_b"`
	Instances int    `json:"instances"`
}

type Capability struct {
	Group     string `json:"group"` // simd|crypto|virtualization|security
	Name      string `json:"name"`  // "AVX2", "AES-NI", "AMD-V", "SMEP"
	Supported bool   `json:"supported"`
}

type Vulnerability struct {
	Name   string `json:"name"`   // "spectre_v2"
	State  string `json:"state"`  // not_affected|mitigated|vulnerable|unknown
	Detail string `json:"detail"` // texto crudo del kernel
}

type LogicalCPU struct {
	ID      int      `json:"id"`
	Core    int      `json:"core"`
	Package int      `json:"package"`
	Die     int      `json:"die"`
	L3      int      `json:"l3"` // índice del grupo de L3 (CCX/CCD) en orden de aparición
	Sibling []int    `json:"siblings"`
	MaxMHz  *float64 `json:"max_mhz,omitempty"` // distinto por núcleo en híbridos (E-cores)
}

type Memory struct {
	TotalB     uint64   `json:"total_b"`
	UsedB      uint64   `json:"used_b"` // total - free - buffers - cached - sreclaimable (como free(1))
	AvailableB uint64   `json:"available_b"`
	FreeB      uint64   `json:"free_b"`
	CacheB     uint64   `json:"cache_b"` // buffers + cached + sreclaimable
	DirtyB     uint64   `json:"dirty_b"`
	ShmemB     uint64   `json:"shmem_b"`
	UsedPct    float64  `json:"used_pct"`
	Swaps      []Swap   `json:"swaps,omitempty"`
	SwapTotalB uint64   `json:"swap_total_b"`
	SwapUsedB  uint64   `json:"swap_used_b"`
	PSI        *PSI     `json:"psi,omitempty"`
	Slots      int      `json:"slots,omitempty"`
	CapacityB  uint64   `json:"capacity_b,omitempty"`
	ECC        string   `json:"ecc,omitempty"`
	Modules    []Module `json:"modules,omitempty"`
	OOMKills   *uint64  `json:"oom_kills,omitempty"` // oom_kill de /proc/vmstat: los que el kernel mató por falta de memoria desde el arranque
}

type Swap struct {
	Name      string   `json:"name"` // "/dev/zram0", "/swap/swapfile"
	Kind      string   `json:"kind"` // partition|file|zram
	SizeB     uint64   `json:"size_b"`
	UsedB     uint64   `json:"used_b"`
	Priority  int      `json:"priority"`
	ZramAlgo  string   `json:"zram_algo,omitempty"`
	ZramRatio *float64 `json:"zram_ratio,omitempty"` // orig/compr
}

type Module struct {
	Slot      string `json:"slot"`
	Type      string `json:"type"` // DDR4|DDR5|LPDDR5|…
	SizeB     uint64 `json:"size_b"`
	SpecMTs   *int   `json:"spec_mts,omitempty"`
	ActualMTs *int   `json:"actual_mts,omitempty"` // crudo, como lo informa DMI
}

// PCIeLink: los cur_* se omiten si no se conocen (GPU suspendida).
type PCIeLink struct {
	CurGTs   float64 `json:"cur_gts,omitempty"`
	MaxGTs   float64 `json:"max_gts"`
	CurGen   int     `json:"cur_gen,omitempty"`
	MaxGen   int     `json:"max_gen"`
	CurWidth int     `json:"cur_width,omitempty"`
	MaxWidth int     `json:"max_width"`
}

type GPU struct {
	Card          string    `json:"card"`   // "card1"
	PCI           string    `json:"pci"`    // "0000:42:00.0"
	Vendor        string    `json:"vendor"` // nvidia|amd|intel|other
	VendorID      string    `json:"vendor_id"`
	DeviceID      string    `json:"device_id"`
	Name          string    `json:"name"`
	Driver        string    `json:"driver"`
	DriverVersion string    `json:"driver_version,omitempty"`
	VBIOS         string    `json:"vbios,omitempty"`
	Suspended     bool      `json:"suspended"`
	Integrated    bool      `json:"integrated"`
	Link          *PCIeLink `json:"link,omitempty"`
	UsagePct      *float64  `json:"usage_pct,omitempty"`
	VRAMUsedB     *uint64   `json:"vram_used_b,omitempty"`
	VRAMTotalB    *uint64   `json:"vram_total_b,omitempty"`
	GTTUsedB      *uint64   `json:"gtt_used_b,omitempty"`
	GTTTotalB     *uint64   `json:"gtt_total_b,omitempty"`
	TempC         *float64  `json:"temp_c,omitempty"`
	PowerW        *float64  `json:"power_w,omitempty"`
	PowerLimitW   *float64  `json:"power_limit_w,omitempty"`
	CoreMHz       *float64  `json:"core_mhz,omitempty"`
	MemMHz        *float64  `json:"mem_mhz,omitempty"`
	FanPct        *float64  `json:"fan_pct,omitempty"`
	Processes     []GPUProc `json:"processes,omitempty"`
}

type GPUProc struct {
	PID   int    `json:"pid"`
	Name  string `json:"name"`
	VRAMB uint64 `json:"vram_b"`
}

type Storage struct {
	Disks       []Disk       `json:"disks"`
	Filesystems []Filesystem `json:"filesystems"`
	PSI         *PSI         `json:"psi,omitempty"`
}

type Disk struct {
	Name       string      `json:"name"` // "nvme1n1"
	Model      string      `json:"model"`
	Serial     string      `json:"serial,omitempty"`
	Transport  string      `json:"transport"` // nvme|sata|usb|…
	Rotational bool        `json:"rotational"`
	SizeB      uint64      `json:"size_b"`
	TempC      *float64    `json:"temp_c,omitempty"`
	ReadBps    *float64    `json:"read_bps,omitempty"`
	WriteBps   *float64    `json:"write_bps,omitempty"`
	BusyPct    *float64    `json:"busy_pct,omitempty"`
	Smart      *Smart      `json:"smart,omitempty"`
	Partitions []Partition `json:"partitions,omitempty"`
}

type Partition struct {
	Name   string   `json:"name"`
	SizeB  uint64   `json:"size_b"`
	FSType string   `json:"fstype,omitempty"`
	Mounts []string `json:"mounts,omitempty"`
	Holder string   `json:"holder,omitempty"` // "root" (dm-crypt / LVM que la usa)
}

type Filesystem struct {
	Device  string   `json:"device"` // "/dev/mapper/root"
	Disks   []string `json:"disks"`  // discos físicos debajo: ["nvme1n1"]
	FSType  string   `json:"fstype"`
	Mounts  []string `json:"mounts"` // subvolúmenes btrfs del mismo dispositivo juntos
	SizeB   uint64   `json:"size_b"`
	UsedB   uint64   `json:"used_b"`
	FreeB   uint64   `json:"free_b"`
	UsedPct float64  `json:"used_pct"`
	Crypt   bool     `json:"crypt"`
	LVM     bool     `json:"lvm"`
}

type Smart struct {
	Kind         string    `json:"kind"` // nvme|ata
	Failing      bool      `json:"failing"`
	Warnings     []string  `json:"warnings,omitempty"`
	PercentUsed  *int      `json:"percent_used,omitempty"`
	PowerOnHours *int      `json:"power_on_hours,omitempty"`
	TempC        *float64  `json:"temp_c,omitempty"`
	BadSectors   *int      `json:"bad_sectors,omitempty"`
	MediaErrors  *int      `json:"media_errors,omitempty"`
	Updated      time.Time `json:"updated"`
}

type Network struct {
	Interfaces   []Iface  `json:"interfaces"`
	RxBps        float64  `json:"rx_bps"` // solo interfaces físicas activas
	TxBps        float64  `json:"tx_bps"`
	Gateway      string   `json:"gateway,omitempty"`
	GatewayIface string   `json:"gateway_iface,omitempty"`
	DNS          []string `json:"dns,omitempty"`
}

type Iface struct {
	Name      string   `json:"name"`
	Kind      string   `json:"kind"` // ethernet|wifi|loopback|bridge|vpn|virtual
	Virtual   bool     `json:"virtual"`
	Up        bool     `json:"up"`
	MAC       string   `json:"mac,omitempty"`
	SpeedMbps *int     `json:"speed_mbps,omitempty"`
	IPv4      []string `json:"ipv4,omitempty"`
	IPv6      []string `json:"ipv6,omitempty"`
	RxBps     *float64 `json:"rx_bps,omitempty"`
	TxBps     *float64 `json:"tx_bps,omitempty"`
	RxTotalB  uint64   `json:"rx_total_b"`
	TxTotalB  uint64   `json:"tx_total_b"`
	Wifi      *Wifi    `json:"wifi,omitempty"`
}

type Wifi struct {
	SSID        string   `json:"ssid,omitempty"`
	SignalDBm   *int     `json:"signal_dbm,omitempty"`
	BitrateMbps *float64 `json:"bitrate_mbps,omitempty"`
	// Lo que sigue lo lee el paquete wifi con nmcli, solo con la vista Red
	// abierta.
	Manager    string    `json:"manager,omitempty"`    // "nm" o "none" (sin NetworkManager); "" = sin leer
	Radio      string    `json:"radio,omitempty"`      // on|soft|hard
	State      string    `json:"state,omitempty"`      // el de nmcli: connected, disconnected, unavailable, connecting (…)
	Connecting string    `json:"connecting,omitempty"` // la red a la que se está conectando
	Networks   []WifiNet `json:"networks,omitempty"`   // vacío con la placa apagada o unavailable
	Scanned    time.Time `json:"scanned,omitzero"`     // hora de la última lectura buena
	Error      string    `json:"error,omitempty"`      // la última lectura falló: el texto de nmcli
}

// WifiNet es una red cercana: una por SSID; las ocultas (sin SSID), una por
// BSSID.
type WifiNet struct {
	SSID     string `json:"ssid"`
	Hidden   bool   `json:"hidden,omitempty"`   // no anuncia su nombre: SSID vacío
	BSSID    string `json:"bssid,omitempty"`    // solo en las ocultas: AA:BB:CC:DD:EE:FF
	Signal   int    `json:"signal"`             // 0..100
	Band     string `json:"band,omitempty"`     // "2.4", "5" o "6"
	Security string `json:"security,omitempty"` // "" = abierta
	Saved    bool   `json:"saved,omitempty"`
	UUID     string `json:"uuid,omitempty"` // de la conexión guardada
	InUse    bool   `json:"in_use,omitempty"`
}

type Sensors struct {
	Chips []Chip `json:"chips"`
}

type Chip struct {
	ID    string    `json:"id"`    // "hwmon2"
	Name  string    `json:"name"`  // "k10temp"
	Label string    `json:"label"` // legible: "CPU", "NVMe Samsung SSD 960 PRO", "Motherboard"
	Kind  string    `json:"kind"`  // cpu|gpu|nvme|ram|board|wifi|nic|battery|other
	Temps []Reading `json:"temps,omitempty"`
	Fans  []Reading `json:"fans,omitempty"`
	Volts []Reading `json:"volts,omitempty"`
}

type Reading struct {
	Label string   `json:"label"`
	Value float64  `json:"value"` // °C, RPM o V
	High  *float64 `json:"high,omitempty"`
	Crit  *float64 `json:"crit,omitempty"`
}

type Power struct {
	Profile  string   `json:"profile,omitempty"` // power-saver|balanced|performance
	Profiles []string `json:"profiles,omitempty"`
	// SavedAC y SavedBattery son los perfiles que Omarchy guardó para cada
	// fuente (omarchy-powerprofiles-set); "" = sin preferencia.
	SavedAC      string `json:"saved_ac,omitempty"`
	SavedBattery string `json:"saved_battery,omitempty"`
	// SetScript dice si está omarchy-powerprofiles-set (recuerda el perfil
	// por fuente; sin él, el cambio va directo a powerprofilesctl).
	SetScript bool          `json:"set_script,omitempty"`
	OnAC      *bool         `json:"on_ac,omitempty"`
	Batteries []Battery     `json:"batteries,omitempty"`
	RAPL      []PowerDomain `json:"rapl,omitempty"`
	// Inhibit es qué impide suspender o apagar la pantalla; se lee solo con
	// Energía abierta (nil = no se miró).
	Inhibit *Inhibit `json:"inhibit,omitempty"`
	// SleepMode es el modo de suspensión en uso (el que está entre corchetes
	// en /sys/power/mem_sleep: deep|s2idle|shallow); SleepModes, todos los
	// que ofrece el kernel, en su orden.
	SleepMode  string       `json:"sleep_mode,omitempty"`
	SleepModes []string     `json:"sleep_modes,omitempty"`
	Wakeup     []WakeDevice `json:"wakeup,omitempty"` // lo que puede despertar el equipo
}

type Battery struct {
	Name           string   `json:"name"`
	Model          string   `json:"model,omitempty"`
	Manufacturer   string   `json:"manufacturer,omitempty"`
	Peripheral     bool     `json:"peripheral"` // scope=Device (mouse, auriculares)
	Pct            float64  `json:"pct"`
	Status         string   `json:"status"` // Charging|Discharging|Full|Not charging|Unknown
	HealthPct      *float64 `json:"health_pct,omitempty"`
	Cycles         *int     `json:"cycles,omitempty"`
	PowerW         *float64 `json:"power_w,omitempty"`
	TimeLeftS      *int     `json:"time_left_s,omitempty"`
	ChargeLimitPct *int     `json:"charge_limit_pct,omitempty"`
	FullWh         *float64 `json:"full_wh,omitempty"`
	DesignWh       *float64 `json:"design_wh,omitempty"`
}

type PowerDomain struct {
	Name  string  `json:"name"` // package-0|core|uncore|dram
	Watts float64 `json:"watts"`
}

// Inhibit: lo que impide suspender o apagar la pantalla. Las listas vacías
// son [] (se miró y no hay nada).
type Inhibit struct {
	Logind    []Inhibitor  `json:"logind"`     // org.freedesktop.login1.Manager.ListInhibitors
	Windows   []IdleWindow `json:"windows"`    // ventanas de Hyprland con inhibitingIdle
	StayAwake bool         `json:"stay_awake"` // "mantener despierto" de Omarchy
}

// Inhibitor es un inhibidor de logind: qué frena (sleep, idle,
// handle-lid-switch…), quién y por qué, y si bloquea o solo demora.
type Inhibitor struct {
	What []string `json:"what"`
	Who  string   `json:"who"`
	Why  string   `json:"why"`
	Mode string   `json:"mode"` // block|block-weak|delay
	UID  uint32   `json:"uid"`
	PID  uint32   `json:"pid"`
}

// IdleWindow es una ventana que mantiene la pantalla encendida.
type IdleWindow struct {
	Class string `json:"class"`
	Title string `json:"title"`
}

// WakeDevice puede despertar el equipo (power/wakeup = enabled).
type WakeDevice struct {
	Name string `json:"name"`
	Kind string `json:"kind"` // usb|net
	Path string `json:"path"` // 1-5, 0000:05:00.0
}

type System struct {
	Hostname    string      `json:"hostname"`
	OS          string      `json:"os"`
	Omarchy     string      `json:"omarchy,omitempty"`
	Kernel      string      `json:"kernel"`
	BootTime    time.Time   `json:"boot_time"`
	UptimeS     float64     `json:"uptime_s"`
	Product     DMIProduct  `json:"product"`
	Board       DMIBoard    `json:"board"`
	BIOS        DMIBIOS     `json:"bios"`
	Chassis     string      `json:"chassis,omitempty"` // Desktop|Laptop|Notebook|…
	Packages    Packages    `json:"packages"`
	Boot        *BootTiming `json:"boot,omitempty"`
	FailedUnits []Unit      `json:"failed_units"`
	PCI         []PCIDevice `json:"pci,omitempty"`
	USB         []USBDevice `json:"usb,omitempty"`
	Bluetooth   []BTDevice  `json:"bluetooth,omitempty"`
	BTPresent   bool        `json:"bt_present,omitempty"` // hay algún adaptador (org.bluez.Adapter1)
	BTPowered   bool        `json:"bt_powered,omitempty"` // algún adaptador encendido
	BTBlocked   bool        `json:"bt_blocked,omitempty"` // bloqueado por rfkill (soft)
	BTTools     bool        `json:"bt_tools,omitempty"`   // omarchy-bluetooth-power y -device en el PATH
	Monitors    []Monitor   `json:"monitors,omitempty"`
	Audio       []string    `json:"audio,omitempty"`
	Sound       *Sound      `json:"sound,omitempty"` // salidas y entradas (pactl); solo con Dispositivos abierta
	SecureBoot  *bool       `json:"secure_boot,omitempty"`
	TPM         string      `json:"tpm,omitempty"`  // "2.0", "1.2" o ""
	Time        *TimeSync   `json:"time,omitempty"` // nil si no hay zona, adjtimex ni timesyncd
}

// TimeSync es la zona horaria y la sincronización del reloj (lo mismo que
// timedate1, leído sin activarlo: ver system.clock).
type TimeSync struct {
	Zone   string `json:"zone,omitempty"`
	NTP    bool   `json:"ntp"`     // sincronización automática activada
	Synced bool   `json:"synced"`  // el reloj está sincronizado
	CanNTP bool   `json:"can_ntp"` // hay un servicio de NTP para activar
}

type DMIProduct struct {
	Vendor  string `json:"vendor,omitempty"`
	Name    string `json:"name,omitempty"`
	Family  string `json:"family,omitempty"`
	Version string `json:"version,omitempty"`
}

type DMIBoard struct {
	Vendor  string `json:"vendor,omitempty"`
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

type DMIBIOS struct {
	Vendor  string `json:"vendor,omitempty"`
	Version string `json:"version,omitempty"`
	Date    string `json:"date,omitempty"`
	Release string `json:"release,omitempty"`
}

type Packages struct {
	Pacman  int  `json:"pacman"`
	Foreign *int `json:"foreign,omitempty"` // AUR y locales (pacman -Qm)
	Flatpak int  `json:"flatpak"`
}

type BootTiming struct {
	FirmwareS  float64 `json:"firmware_s,omitempty"`
	LoaderS    float64 `json:"loader_s,omitempty"`
	KernelS    float64 `json:"kernel_s,omitempty"`
	InitrdS    float64 `json:"initrd_s,omitempty"`
	UserspaceS float64 `json:"userspace_s,omitempty"`
	TotalS     float64 `json:"total_s,omitempty"`
}

type Unit struct {
	Name        string `json:"name"`
	Scope       string `json:"scope"` // system|user
	Description string `json:"description"`
}

type PCIDevice struct {
	Slot     string `json:"slot,omitempty"`
	Class    string `json:"class,omitempty"`
	Vendor   string `json:"vendor,omitempty"`
	Device   string `json:"device,omitempty"`
	VendorID string `json:"vendor_id,omitempty"`
	DeviceID string `json:"device_id,omitempty"`
	Driver   string `json:"driver,omitempty"`
}

type USBDevice struct {
	Path      string  `json:"path,omitempty"`
	Vendor    string  `json:"vendor,omitempty"`
	Product   string  `json:"product,omitempty"`
	VendorID  string  `json:"vendor_id,omitempty"`
	ProductID string  `json:"product_id,omitempty"`
	Driver    string  `json:"driver,omitempty"`
	SpeedMbps float64 `json:"speed_mbps,omitempty"`
	Version   string  `json:"version,omitempty"` // bcdUSB que anuncia: "2.00", "3.10"
	SSPort    bool    `json:"ss_port,omitempty"` // su puerto tiene par SuperSpeed (port/peer)
	Class     string  `json:"class,omitempty"`   // bInterfaceClass de la primera interfaz: "08", "0e", "03"…
}

type BTDevice struct {
	Name      string `json:"name"`
	Address   string `json:"address"`
	Connected bool   `json:"connected"`
	Paired    bool   `json:"paired,omitempty"`  // emparejado (si no, "cerca": BlueZ lo recuerda de una búsqueda)
	Trusted   bool   `json:"trusted,omitempty"` // de confianza
	Battery   *int   `json:"battery_pct,omitempty"`
}

// Sound son las salidas y entradas de audio de PipeWire (pactl), como las
// ofrece la barra: sin las salidas que Omarchy marca no disponibles ni los
// monitores.
type Sound struct {
	Outputs []AudioNode `json:"outputs"`
	Inputs  []AudioNode `json:"inputs"`
}

// AudioNode es una salida (sink) o una entrada (source).
type AudioNode struct {
	ID      string `json:"id"`     // object.id de PipeWire (lo piden los scripts de Omarchy)
	Name    string `json:"name"`   // nombre de pactl (alsa_output.…)
	Label   string `json:"label"`  // node.nick, si no la descripción
	Volume  int    `json:"volume"` // %, promedio de los canales
	Muted   bool   `json:"muted,omitempty"`
	Default bool   `json:"default,omitempty"`
}

type Monitor struct {
	Connector    string       `json:"connector,omitempty"`
	Manufacturer string       `json:"manufacturer,omitempty"`
	Model        string       `json:"model,omitempty"`
	Serial       string       `json:"serial,omitempty"`
	Connected    bool         `json:"connected"`
	WidthMM      int          `json:"width_mm,omitempty"`
	HeightMM     int          `json:"height_mm,omitempty"`
	Preferred    string       `json:"preferred,omitempty"` // "2560x1440@143.91"
	MaxVHz       int          `json:"max_v_hz,omitempty"`  // frecuencia vertical máxima del rango del EDID (0xFD)
	Live         *MonitorMode `json:"live,omitempty"`      // el modo en uso según Hyprland; nil sin Hyprland
}

// MonitorMode es el modo en uso de un monitor (j/monitors de Hyprland).
type MonitorMode struct {
	Width     int     `json:"width"`
	Height    int     `json:"height"`
	RefreshHz float64 `json:"refresh_hz"`
	Scale     float64 `json:"scale"`
	VRR       bool    `json:"vrr"`
	Format    string  `json:"format,omitempty"`       // XRGB8888
	Preset    string  `json:"color_preset,omitempty"` // srgb
	Disabled  bool    `json:"disabled,omitempty"`
	MirrorOf  string  `json:"mirror_of,omitempty"` // conector al que espeja; "" = ninguno
	BestHz    float64 `json:"best_hz,omitempty"`   // la mayor de availableModes a Width×Height; 0 = no la ofrece
}

// Privacy: quién usa la cámara o el micrófono. Las listas vacías son [] (se
// miró y no hay nada), a diferencia de un Privacy nil (no se miró).
type Privacy struct {
	Camera []string `json:"camera"` // nombres (comm) de los procesos que tienen abierta una cámara
	Mic    []string `json:"mic"`    // idem para una captura de audio en curso
	// MicActive: hay una captura en curso aunque Mic esté vacío (la tiene el
	// servidor de audio, que no dice qué app graba).
	MicActive bool `json:"mic_active"`
}

type Firmware struct {
	Checked time.Time        `json:"checked"`
	Devices []FirmwareDevice `json:"devices"`
}

type FirmwareDevice struct {
	Name      string `json:"name,omitempty"`
	Current   string `json:"current,omitempty"`
	Available string `json:"available,omitempty"`
	Vendor    string `json:"vendor,omitempty"`
}

type Self struct {
	TickMs   float64 `json:"tick_ms"` // tiempo de CPU del último tick
	AvgMs    float64 `json:"avg_ms"`  // promedio móvil de 30 ticks
	RSSB     uint64  `json:"rss_b"`
	Interval float64 `json:"interval_s"`
}

// Apps son las aplicaciones del usuario (procesos agrupados) y sus ventanas
// de Hyprland. List viene ordenada: primero las que tienen ventana, cada
// parte por CPU.
type Apps struct {
	List        []App    `json:"list"`
	Monitors    []string `json:"monitors,omitempty"`
	Frozen      []Frozen `json:"frozen,omitempty"`
	TotalCPUPct float64  `json:"total_cpu_pct"`
	TotalMemB   uint64   `json:"total_mem_b"` // suma de RSS: aproximada
}

type App struct {
	ID       string   `json:"id"` // scope de systemd o "comm:<nombre>"
	Name     string   `json:"name"`
	Procs    int      `json:"procs"`
	CPUPct   float64  `json:"cpu_pct"` // 100 = toda la máquina
	MemB     uint64   `json:"mem_b"`   // suma de RSS: aproximada
	GPUPct   *float64 `json:"gpu_pct,omitempty"`
	NetRxBps *float64 `json:"net_rx_bps,omitempty"` // solo TCP
	NetTxBps *float64 `json:"net_tx_bps,omitempty"`
	// Bytes por segundo que la app lee del disco y escribe en él
	// (/proc/<pid>/io read_bytes y write_bytes − cancelled_write_bytes).
	// Solo con Aplicaciones abierta; nil = sin dato.
	DiskReadBps  *float64 `json:"disk_read_bps,omitempty"`
	DiskWriteBps *float64 `json:"disk_write_bps,omitempty"`
	Frozen       bool     `json:"frozen,omitempty"`
	Protected    bool     `json:"protected,omitempty"`
	Windows      []Window `json:"windows,omitempty"`
}

type Window struct {
	Address     string `json:"address"`
	StableID    string `json:"stable_id,omitempty"` // stableId de Hyprland 0.56: no se reusa como la dirección
	Title       string `json:"title"`
	Class       string `json:"class"`
	Workspace   string `json:"workspace"`
	Monitor     string `json:"monitor"`
	Floating    bool   `json:"floating"`
	Pinned      bool   `json:"pinned"`
	Fullscreen  int    `json:"fullscreen"` // 0 no, 1 maximizada, 2 pantalla completa
	XWayland    bool   `json:"xwayland"`
	Grouped     bool   `json:"grouped"`
	Focused     bool   `json:"focused"`
	InhibitIdle bool   `json:"inhibit_idle"`
	Size        [2]int `json:"size"`
	At          [2]int `json:"at"`
}

// Frozen es una app congelada y cuándo se reanuda sola.
type Frozen struct {
	ID    string    `json:"id"`
	Name  string    `json:"name"`
	Until time.Time `json:"until"`
}

// Maintenance es la sección Mantenimiento. Reboot, Upgrade y Updates se
// leen también con la sección cerrada (el Resumen avisa con ellos); Caches
// y Snapshots solo con la sección abierta, y se conserva lo último leído.
type Maintenance struct {
	Reboot    *Reboot    `json:"reboot,omitempty"`
	Upgrade   *Upgrade   `json:"upgrade,omitempty"`
	Updates   *Updates   `json:"updates,omitempty"` // solo después de una comprobación (u)
	Caches    []CacheDir `json:"caches,omitempty"`
	CachesAt  time.Time  `json:"caches_at,omitzero"`  // cuándo se midieron
	Snapshots *Snapshots `json:"snapshots,omitempty"` // nil: sin snapper o / no es btrfs
	Health    *Health    `json:"health,omitempty"`    // solo con Mantenimiento abierta (los OOM, también con Memoria)
}

// Reboot dice si hace falta reiniciar, con las reglas de
// omarchy-update-restart.
type Reboot struct {
	Kernel          string   `json:"kernel"`                      // el que corre (uname -r)
	Installed       []string `json:"installed"`                   // los que tienen vmlinuz en /usr/lib/modules
	KernelGone      bool     `json:"kernel_gone"`                 // hay vmlinuz instalados y no el del kernel en uso
	Marker          bool     `json:"marker"`                      // ~/.local/state/omarchy/reboot-required
	Hyprland        *bool    `json:"hyprland_replaced,omitempty"` // nil: sin Hyprland
	Services        []string `json:"services,omitempty"`          // restart-<servicio>-required
	NVIDIALoaded    string   `json:"nvidia_loaded,omitempty"`     // /sys/module/nvidia/version
	NVIDIAInstalled string   `json:"nvidia_installed,omitempty"`  // del paquete nvidia(-<rama>)-utils
}

// Upgrade sale de /var/log/pacman.log. Tiempo cero: no hay registro.
type Upgrade struct {
	LastFull     time.Time `json:"last_full,omitzero"`   // último "starting full system upgrade"
	FullPackages int       `json:"full_packages"`        // "upgraded" de esa actualización
	LastChange   time.Time `json:"last_change,omitzero"` // último "transaction completed"
}

// Updates es el resultado de la última comprobación (checkupdates, a pedido).
type Updates struct {
	Checked time.Time   `json:"checked,omitzero"`
	Repo    []PkgUpdate `json:"repo"`
	Error   string      `json:"error,omitempty"`
	Stale   bool        `json:"stale,omitempty"`   // hubo un cambio de paquetes después de Checked
	Running bool        `json:"running,omitempty"` // hay una comprobación en curso
}

type PkgUpdate struct {
	Name string `json:"name"`
	Old  string `json:"old"`
	New  string `json:"new"`
	Repo string `json:"repo,omitempty"` // core, extra, …; "" si no se pudo saber
}

// CacheDir es lo que ocupa en disco una caché (id: pacman, yay, paru,
// journal, coredump).
type CacheDir struct {
	ID      string `json:"id"`
	Path    string `json:"path,omitempty"`
	SizeB   uint64 `json:"size_b"`
	Partial bool   `json:"partial,omitempty"` // algo no se pudo leer o se llegó al tope
}

// Snapshots son los configs de snapper y, si el usuario tiene permiso,
// sus snapshots.
type Snapshots struct {
	User    string       `json:"user"`
	Configs []SnapConfig `json:"configs"`
}

type SnapConfig struct {
	Name       string `json:"name"`
	Allowed    bool   `json:"allowed"`
	AllowUsers string `json:"allow_users,omitempty"` // ALLOW_USERS tal cual (para el comando de habilitar)
	Total      int    `json:"total"`
	List       []Snap `json:"list"` // las 10 más nuevas; null si no se leyó
}

// Snap no se llama Snapshot: ese nombre es la muestra entera.
type Snap struct {
	Number      uint32    `json:"number"`
	Date        time.Time `json:"date"`
	Type        string    `json:"type"` // single|pre|post
	Description string    `json:"description,omitempty"`
	Cleanup     string    `json:"cleanup,omitempty"`
}

// Health es la salud del sistema en este arranque: los errores del journal,
// los programas que se cerraron (coredumpctl) y los procesos que se mataron
// por falta de memoria. Cada parte es nil si no se leyó o falló (el
// unavailable maintenance.health.* dice por qué). Se conserva lo último
// leído al salir de la sección.
type Health struct {
	Errors  *JournalErrors `json:"errors,omitempty"`
	Crashes *Crashes       `json:"crashes,omitempty"`
	OOM     *OOMList       `json:"oom,omitempty"`
}

// JournalErrors son los errores (prioridad err o más grave) de este arranque.
type JournalErrors struct {
	Count   int            `json:"count"`
	Capped  bool           `json:"capped,omitempty"`  // llegó al tope de 1000: "1000+"
	Partial bool           `json:"partial,omitempty"` // el usuario ve solo sus mensajes
	Sources []ErrorSource  `json:"sources"`           // de más a menos
	Recent  []JournalEntry `json:"recent"`            // las 5 más recientes, la más nueva primero
}

type ErrorSource struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type JournalEntry struct {
	Time     time.Time `json:"time"`
	Source   string    `json:"source"`
	Priority int       `json:"priority"`
	Message  string    `json:"message"` // una línea, hasta 160 runas
}

// Crashes son los volcados de los últimos 7 días (coredumpctl).
type Crashes struct {
	Total int     `json:"total"`
	List  []Crash `json:"list"` // los 10 más recientes, el más nuevo primero
}

type Crash struct {
	Time       time.Time `json:"time"`
	PID        int       `json:"pid"`
	Program    string    `json:"program"` // la base de exe
	Signal     int       `json:"signal"`
	SignalName string    `json:"signal_name,omitempty"` // SIGILL, SIGSEGV…
	UID        uint32    `json:"uid"`
	User       string    `json:"user"`
	Stored     bool      `json:"stored"` // el volcado está guardado (corefile present o journal)
}

// OOMList son los procesos que se mataron por falta de memoria en este
// arranque, con nombre: los del kernel (solo se buscan si oom_kill > 0) y
// los de systemd-oomd (que no suman a oom_kill).
type OOMList struct {
	Oomd    int       `json:"oomd"`              // cuántas mató systemd-oomd (de las 50 más nuevas)
	List    []OOMKill `json:"list"`              // la más nueva primero
	Partial bool      `json:"partial,omitempty"` // el usuario ve solo sus mensajes: las de systemd-oomd y los nombres del kernel no se ven
}

type OOMKill struct {
	Time    time.Time `json:"time"`
	Program string    `json:"program"`
	By      string    `json:"by"` // kernel|systemd-oomd
}

// Security es lo de la sección Seguridad que no es la cámara ni el
// micrófono. Se lee con lo lento, también con la sección cerrada (el
// Resumen avisa con él); los programas de los sockets propios, solo con la
// sección abierta.
type Security struct {
	Listening []Listener    `json:"listening"` // null: /proc/net no se pudo leer
	Firewall  *Firewall     `json:"firewall,omitempty"`
	Volumes   []CryptVolume `json:"volumes,omitempty"`
	Swaps     []SwapCrypt   `json:"swaps,omitempty"`
}

// Listener es un puerto que escucha: los sockets con el mismo protocolo,
// dirección y puerto (un servicio en IPv4 e IPv6 son dos).
type Listener struct {
	Proto    string          `json:"proto"` // tcp|udp
	Addr     string          `json:"addr"`  // "0.0.0.0", "::", "127.0.0.1", "fe80::1"
	Port     int             `json:"port"`
	Scope    string          `json:"scope"`           // local|all|link|multicast|addr
	Iface    string          `json:"iface,omitempty"` // la interfaz que tiene Addr (link, addr)
	Owners   []ListenerOwner `json:"owners"`
	Firewall string          `json:"firewall,omitempty"` // solo scope all: open|limited|blocked|none|unknown
	From     string          `json:"from,omitempty"`     // firewall limited: los orígenes permitidos
}

// ListenerOwner es de quién es un socket: el uid y su nombre, y el programa
// si es del usuario y se buscó (Seguridad abierta).
type ListenerOwner struct {
	UID     uint32 `json:"uid"`
	User    string `json:"user,omitempty"`
	Program string `json:"program,omitempty"`
	Own     bool   `json:"own,omitempty"`
}

// Firewall es el estado del firewall: ufw (sus archivos) o, sin ufw
// cargado, firewalld o nftables (solo si corren; sus reglas no se leen).
type Firewall struct {
	Tool         string   `json:"tool,omitempty"`          // ufw|firewalld|nftables; "" = ninguno
	State        string   `json:"state"`                   // on|running|not_loaded|off|none
	Active       bool     `json:"active"`                  // filtra lo que entra: ufw on sin ACCEPT, o running
	Policy       string   `json:"policy,omitempty"`        // DEFAULT_INPUT_POLICY de ufw: DROP|REJECT|ACCEPT
	IPv6         bool     `json:"ipv6"`                    // ufw filtra también IPv6
	Rules        []FwRule `json:"rules,omitempty"`         // reglas de entrada de ufw, en orden
	RulesUnknown bool     `json:"rules_unknown,omitempty"` // user.rules o user6.rules existen pero no se pudieron leer: los veredictos son unknown, no blocked
}

// FwRule es una regla de entrada de ufw (una tupla de user.rules o
// user6.rules).
type FwRule struct {
	Action  string `json:"action"` // allow|deny|reject|limit
	Proto   string `json:"proto"`  // tcp|udp|any
	Port    string `json:"port"`   // "1716", "1714:1764", "80,443", "any"
	To      string `json:"to"`     // destino: "0.0.0.0/0", "::/0" o una dirección
	From    string `json:"from"`   // origen
	Iface   string `json:"iface,omitempty"`
	V6      bool   `json:"v6,omitempty"`
	App     string `json:"app,omitempty"`
	Comment string `json:"comment,omitempty"`
}

// CryptVolume dice si un sistema de archivos montado está cifrado.
type CryptVolume struct {
	Device    string   `json:"device"`
	Mounts    []string `json:"mounts"`
	Encrypted bool     `json:"encrypted"`
	Type      string   `json:"type,omitempty"` // LUKS2|LUKS1|PLAIN|BITLK|TCRYPT
	On        string   `json:"on,omitempty"`   // la partición física de abajo: "nvme1n1p2"
}

// SwapCrypt dice si una swap activa está cifrada.
type SwapCrypt struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`  // partition|file|zram
	State string `json:"state"` // encrypted|plain|memory|unknown
	Type  string `json:"type,omitempty"`
	On    string `json:"on,omitempty"`
}
