// Command sysoscope es el colector de Sysoscope: inventario y métricas del
// sistema en JSON, sin root.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/engine"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/history"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

var version = "dev"

const usage = `usage: sysoscope <command> [flags]

commands:
  snapshot [--section S] [--pretty] [--lang en|es]
             print one snapshot as JSON (reuses the stream's latest.json when fresh)
  stream [--interval 2s] [--lang en|es] [--units c|f] [--cards a,b]
             print one JSON snapshot per line, with "sections" and, when a
             view is focused, "view"; stdin accepts:
             interval N | focus [VIEW] | pause | resume | refresh | quit |
             lang en|es | units c|f | cards a,b | action firmware.check |
             action updates.check |
             action ID ["ROW","INPUT"] (a window.*, app.*, wifi.*,
             power.*, audio.* or bt.* action; the next line carries
             "result") | search "QUERY" (a JSON string; answered
             right away by a line that carries "search")
  view VIEW [--lang en|es] [--units c|f] [--cards a,b] [--pretty]
             print one view of the panel as JSON
  sections [--lang en|es]
             print the panel's sections, with their badges
  search QUERY [--lang en|es] [--units c|f] [--pretty]
             search the content of every section but Summary; prints
             {"query", "hits"}: each hit's section and its title, the
             path of block titles, the text and, for a row you can
             pick, its row id
  lazylazy sections | view ID | stream ID | action ID
             lazylazy's tab protocol (language from LAZYLAZY_LANG)
  action ID [ROW] [INPUT]
             run an action of the Applications section (window.* or app.*)
             on a window ("0x…" or "0x…:STABLEID") or an app ("app:ID"),
             or of the Wi-Fi (wifi.*) on a network ("IFACE:SSID", or
             "IFACE:hidden:BSSID" for a hidden one); the
             Wi-Fi actions for the whole section (wifi.radio, wifi.rescan,
             wifi.band) take no ROW: action ID [INPUT]; neither do the
             Power actions power.profile (set the power profile) and
             power.profile_other (save the profile for the other power
             source): action power.profile balanced. On Devices, audio.*
             (default, mute, up, down, volume N) act on an output or
             input by its pactl name, and bt.connect, bt.disconnect and
             bt.forget on a Bluetooth device by its address; bt.power
             and bt.pair take no ROW
  wifi-hidden [--hold] [--lang en|es] IFACE BSSID
             ask for the name of a hidden network and connect to it with
             nmcli, which asks for the password in the terminal; --hold
             waits for Enter before closing
  history --metric M [--span 24h|7d|30d] [--format csv|json]
             export the history of one metric
  fixture capture --out DIR [--anonymize]
             save what the collectors read, to contribute a test fixture
  version    print the version

sections: cpu memory gpu storage network sensors power system firmware self maintenance security
views: summary cpu memory gpu storage network sensors power about maintenance apps devices security
`

// sampler es lo que la CLI usa del Engine (los tests lo reemplazan).
type sampler interface {
	Tick() *model.Snapshot
	SetFocus(string)
	SetInterval(time.Duration)
	SetLang(string)
	Recent(metric string) []float64
	CheckFirmware()
	FirmwareRunning() bool
	Interval() time.Duration
	RefreshSlow()
	RefreshWifi()
	RefreshPower()
	RefreshAudio(resetAvail bool)
	RefreshBluetooth()
	CheckUpdates()
	RefreshMaintenance()
	Close() error
	History() *history.Store
}

type app struct {
	env       source.Env
	ctx       context.Context
	stdin     io.Reader
	stdout    io.Writer
	stderr    io.Writer
	newEngine func(source.Env, engine.Options) sampler
	sleep     func(time.Duration)
}

// maxProcs devuelve el GOMAXPROCS a fijar: 2 salvo que el usuario haya
// elegido otro (0 = no tocar). Con todos los núcleos el runtime crea más
// hilos y memoria sin ganar nada en un monitor.
func maxProcs(env string) int {
	if env != "" {
		return 0
	}
	return 2
}

// gcPercent devuelve el GOGC a fijar: 50 salvo que el usuario haya elegido
// otro (0 = no tocar). El heap vivo ronda 1 MB, así que un GC más frecuente
// casi no cuesta CPU y baja la memoria residente.
func gcPercent(env string) int {
	if env != "" {
		return 0
	}
	return 50
}

func main() {
	if n := maxProcs(os.Getenv("GOMAXPROCS")); n > 0 {
		runtime.GOMAXPROCS(n)
	}
	if n := gcPercent(os.Getenv("GOGC")); n > 0 {
		debug.SetGCPercent(n)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	a := &app{
		env:   source.Real(startLang()),
		ctx:   ctx,
		stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr,
		newEngine: func(env source.Env, o engine.Options) sampler { return engine.New(env, o) },
		sleep:     time.Sleep,
	}
	os.Exit(a.run(os.Args[1:]))
}

func (a *app) run(args []string) int {
	if a.ctx == nil {
		a.ctx = context.Background()
	}
	if len(args) == 0 {
		fmt.Fprint(a.stderr, usage)
		return 2
	}
	switch args[0] {
	case "version", "--version":
		fmt.Fprintln(a.stdout, version)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(a.stdout, usage)
		return 0
	case "snapshot":
		return a.snapshot(args[1:])
	case "stream":
		return a.stream(args[1:])
	case "view":
		return a.view(args[1:])
	case "sections":
		return a.sections(args[1:])
	case "search":
		return a.searchCmd(args[1:])
	case "action":
		return a.actionCmd(args[1:])
	case "lazylazy":
		return a.lazylazy(args[1:])
	case "history":
		return a.history(args[1:])
	case "fixture":
		return a.fixture(args[1:])
	case "wifi-wait":
		return a.wifiWaitCmd(args[1:])
	case "wifi-hidden":
		return a.wifiHiddenCmd(args[1:])
	case "bt-wait":
		return a.btWaitCmd(args[1:])
	}
	fmt.Fprintf(a.stderr, "sysoscope: %s\n\n%s", i18n.F(a.env.Lang, "unknown command %q", args[0]), usage)
	return 2
}

// sysoscopeLangEnv es la variable con la que este proceso deja su idioma
// para cualquier hijo que lance (un wifi-wait o un bt-wait desacoplado).
// env.State no necesita nada así: sale siempre de XDG_STATE_HOME/HOME, que
// un hijo hereda igual. env.Lang, en cambio, puede venir de --lang o de
// LAZYLAZY_LANG, y un wifi-wait es un proceso nuevo, con su línea de
// comandos fija (sin --lang).
const sysoscopeLangEnv = "SYSOSCOPE_LANG"

// startLang es el idioma con el que arranca el proceso: el que dejó en
// sysoscopeLangEnv el proceso que lo lanzó (setLang lo pone al elegir un
// idioma distinto del de las variables de entorno) o, si no hay, el de esas
// variables.
func startLang() string {
	if l := os.Getenv(sysoscopeLangEnv); l != "" {
		return i18n.Norm(l)
	}
	return i18n.Detect(os.Getenv)
}

// setLang aplica --lang (o LAZYLAZY_LANG) si vino, y lo deja en
// sysoscopeLangEnv: si esto lanza un wifi-wait desacoplado, ese proceso
// nuevo arranca en el mismo idioma, y su resultado queda en actions.log en
// ese idioma.
func (a *app) setLang(lang string) {
	if lang != "" {
		a.env.Lang = i18n.Norm(lang)
		os.Setenv(sysoscopeLangEnv, a.env.Lang)
	}
}

func (a *app) fail(format string, args ...any) int {
	fmt.Fprintf(a.stderr, "sysoscope: %s\n", i18n.F(a.env.Lang, format, args...))
	return 2
}
