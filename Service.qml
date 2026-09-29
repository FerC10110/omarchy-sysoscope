import QtQuick
import Quickshell
import Quickshell.Io
import "I18n.js" as I18n
import "Stream.js" as Stream
import "Settings.js" as Settings
import "BarModel.js" as BarModel
import "PanelModel.js" as PanelModel
import "Launcher.js" as Launcher

// Sysoscope service: the single instance that runs the collector. It starts
// `bin/sysoscope-linux-<arch> stream`, keeps the latest line, restarts the
// collector with a growing wait when it stops, and answers IPC. The bar widget
// (one per monitor) and its panel only read from here.
//
// The shell destroys services on every plugin reload; the collector exits when
// its stdin closes, so no process is left behind.
// It also installs the two launcher entries (Launcher.js) and removes them
// when the plugin is disabled or removed.
Item {
  id: service

  readonly property string pluginId: "io.github.ferc10110.sysoscope"
  readonly property string homeDir: Quickshell.env("HOME")
  readonly property string configHome: Quickshell.env("XDG_CONFIG_HOME") || homeDir + "/.config"

  property string language: Settings.languageFromEnv(Quickshell.env("LANG"))
  property var config: Settings.normalizeConfig({})
  property bool configLoaded: false
  property bool languageLoaded: false

  property string arch: ""
  property string binary: ""
  property string problemCode: ""
  property string problemDetail: ""
  readonly property string problem: Stream.problemText(problemCode, problemDetail, service.t)

  property var snapshot: null
  property var view: null
  property var sections: []
  property var recent: ({})
  property var series: ({})
  property string focusId: ""
  readonly property bool ready: snapshot !== null && problemCode === ""

  property int attempt: 0
  property real startedAt: 0

  signal panelRequested(string action, string section)

  function t(s, args) { return I18n.t(s, service.language, args) }

  function pluginPath(relative) {
    var url = String(Qt.resolvedUrl(relative))
    return url.indexOf("file://") === 0 ? decodeURIComponent(url.substring(7)) : url
  }

  // ---- collector

  function start() {
    if (!configLoaded || !languageLoaded || binary === "" || collector.running) return
    collector.command = [pluginPath(binary)].concat(Stream.streamArgs(config, language))
    startedAt = Date.now()
    collector.running = true
  }

  function restart() {
    if (collector.running) collector.running = false   // onExited schedules start()
    else start()
  }

  function send(command) {
    if (collector.running) collector.write(command + "\n")
  }

  property var focusOwner: null

  function setFocus(id, owner) {
    var next = Stream.focusChange({ owner: focusOwner, id: focusId }, owner, id)
    if (!next) return
    focusOwner = next.owner
    focusId = next.id
    view = null
    send("focus " + focusId)
  }

  // The last action's result ({ id, ok, text }), for the panel's footer.
  property var lastResult: null

  // The last content search reply ({ query, hits }), for the panel's filter.
  property var searchResult: null

  function searchContent(query) { send(Stream.searchCommand(query)) }
  function clearSearch() { searchResult = null }

  function runAction(id, row, input) {
    if (id === "refresh") send("refresh")
    else send(Stream.actionCommand(id, row || "", input || ""))
  }

  function onLine(line) {
    var r = Stream.parseLine(line)
    if (!r.ok) {
      if (r.error === "schema") { problemCode = "schema"; collector.running = false }
      return
    }
    if (problemCode === "crashed") problemCode = ""
    if (Stream.isNewSample(snapshot, r.snapshot)) recent = BarModel.push(recent, r.snapshot)
    snapshot = r.snapshot
    sections = r.sections
    view = (r.view && r.view.id === focusId) ? r.view : null
    if (r.result) lastResult = r.result
    if (r.search) searchResult = r.search
  }

  Process {
    id: collector
    stdinEnabled: true
    stdout: SplitParser { onRead: function(line) { service.onLine(line) } }
    onExited: function(code, status) {
      if (service.problemCode === "schema") return          // restarting would not help
      if (Date.now() - service.startedAt > 60000) service.attempt = 0
      var wait = Stream.backoffMs(service.attempt)
      service.attempt++
      service.problemCode = "crashed"
      service.problemDetail = String(Math.round(wait / 1000))
      retryTimer.interval = wait
      retryTimer.restart()
    }
  }

  Timer { id: retryTimer; repeat: false; onTriggered: service.start() }

  // uname -m → arch → binary; then check that it is executable.
  Process {
    id: unameProc
    command: ["uname", "-m"]
    stdout: StdioCollector {
      onStreamFinished: {
        service.arch = Stream.archOf(this.text)
        if (service.arch === "") { service.problemCode = "arch"; service.problemDetail = this.text.trim(); return }
        service.binary = Stream.binaryFor(service.arch)
        checkProc.command = ["test", "-x", service.pluginPath(service.binary)]
        checkProc.running = true
      }
    }
  }

  Process {
    id: checkProc
    onExited: function(code) {
      if (code !== 0) { service.problemCode = "missing"; service.problemDetail = service.binary; service.binary = ""; return }
      service.start()
    }
  }

  // ---- longer histories for the bar's sparklines and tooltips (span ≠ 2m)

  property var barKeys: []          // set by the first bar widget
  property var seriesQueue: []
  // The timer's first run usually comes before the bar has set its keys.
  onBarKeysChanged: fetchSeries()

  function fetchSeries() {
    if (historyProc.running || binary === "" || config.span === "2m") return
    seriesQueue = barKeys.filter(function(k) { return BarModel.METRICS[k] && BarModel.METRICS[k].history })
    nextSeries()
  }

  function nextSeries() {
    if (seriesQueue.length === 0) return
    var key = seriesQueue[0]
    seriesQueue = seriesQueue.slice(1)
    historyProc.key = key
    historyProc.span = config.span
    historyProc.command = [pluginPath(binary)].concat(Stream.historyArgs(BarModel.METRICS[key].history, config.span))
    historyProc.running = true
  }

  Process {
    id: historyProc
    property string key: ""
    property string span: ""
    stdout: StdioCollector {
      onStreamFinished: {
        var next = {}
        for (var k in service.series) next[k] = service.series[k]
        next[historyProc.key] = Stream.seriesFrom(this.text, historyProc.span)
        service.series = next
      }
    }
    onExited: Qt.callLater(service.nextSeries)
  }

  Timer {
    interval: 60000
    repeat: true
    triggeredOnStart: true
    running: service.binary !== "" && service.config.span !== "2m"
    onTriggered: service.fetchSeries()
  }

  // ---- settings

  FileView {
    id: configFile
    path: service.configHome + "/sysoscope/config.json"
    blockLoading: true
    watchChanges: true
    atomicWrites: true
    printErrors: false
    onFileChanged: reload()
    onLoaded: {
      var raw = null
      try { raw = JSON.parse(text()) } catch (e) { raw = null }
      var next = Settings.normalizeConfig(raw)
      // A hand edit of config.json reaches the collector too (our own
      // writes come back here with no change, so they send nothing).
      if (service.configLoaded) service.applyConfig(service.config, next)
      service.config = next
      service.configLoaded = true
      service.start()
    }
    onLoadFailed: { service.configLoaded = true; service.start() }
  }

  function saveConfig(cfg) {
    var before = config
    config = Settings.normalizeConfig(cfg)
    configFile.setText(JSON.stringify(config, null, 2) + "\n")
    applyConfig(before, config)
  }

  function applyConfig(before, after) {
    var change = Stream.configCommands(before, after)
    change.commands.forEach(function(c) { send(c) })
    if (change.spanChanged) { series = ({}); Qt.callLater(fetchSeries) }
  }

  // The section the panel reopens on, kept across reloads.
  function rememberSection(id) {
    if (id && id !== config.section) saveConfig(Object.assign({}, config, { section: id }))
  }

  FileView {
    id: languageFile
    path: service.configHome + "/omarchy/plugin-language.json"
    blockLoading: true
    watchChanges: true
    atomicWrites: true
    printErrors: false
    onFileChanged: reload()
    onLoaded: {
      var raw = null
      try { raw = JSON.parse(text()) } catch (e) { raw = null }
      var lang = raw && raw.language === "es" ? "es" : (raw && raw.language === "en" ? "en" : service.language)
      if (lang !== service.language) { service.language = lang; service.send("lang " + lang) }
      service.languageLoaded = true
      service.start()
    }
    onLoadFailed: { service.languageLoaded = true; service.start() }
  }

  // Shared by every plugin of the user, so changing it here changes them all.
  // Applied here too: a FileView whose file did not exist at load time never
  // reports the change.
  function setLanguage(lang) {
    lang = lang === "es" ? "es" : "en"
    languageFile.setText(JSON.stringify({ language: lang }, null, 2) + "\n")
    if (lang !== language) { language = lang; send("lang " + lang) }
  }

  // ---- launcher entries (SUPER+SPACE): the panel and the lazylazy tab

  readonly property string applicationsDir: Launcher.applicationsDir(Quickshell.env("XDG_DATA_HOME"), homeDir)

  // A launcher entry is a convenience: a failure goes to the shell's log and
  // never reaches the panel.
  Process {
    id: launcherProc
    stderr: StdioCollector {
      onStreamFinished: { if (this.text.trim() !== "") console.warn("sysoscope: launcher entries: " + this.text.trim()) }
    }
    onExited: function(code) { if (code !== 0) console.warn("sysoscope: installing the launcher entries exited with " + code) }
  }

  function installLauncher() {
    launcherProc.command = Launcher.installArgv(applicationsDir, pluginId)
    launcherProc.running = true
  }

  // A full shell restart destroys the old service and creates a new one right
  // away; the old one's removal below waits and pings, but a shell that is
  // still starting may not answer yet, so it can delete the entries right
  // after this service's own install already ran as a no-op. Reinstalling
  // once more, well after the removal window, puts them back — a plain
  // install is a no-op when nothing changed.
  Timer {
    id: reinstallTimer
    interval: 5000
    repeat: false
    onTriggered: service.installLauncher()
  }

  Component.onCompleted: {
    unameProc.running = true
    installLauncher()
    reinstallTimer.running = true
  }
  // Reached on disable, on remove and on every reload. The removal runs
  // detached (this service is going away) and does nothing if the plugin
  // still answers ping after 2 s: a reload has already made a new service.
  Component.onDestruction: {
    collector.running = false
    Quickshell.execDetached(Launcher.removeArgv(applicationsDir, pluginId, 2, Launcher.aliveCommand(pluginId)))
  }

  // ---- IPC: omarchy-shell io.github.ferc10110.sysoscope <function> [arg]

  IpcHandler {
    target: service.pluginId

    function toggle(): string { service.panelRequested("toggle", ""); return "ok" }
    function open(section: string): string {
      if (!PanelModel.knownSection(service.sections, section)) return "unknown section: " + section
      service.panelRequested("open", section)
      return "ok"
    }
    function refresh(): string { service.send("refresh"); return "ok" }
    function span(value: string): string {
      if (Settings.SPANS.indexOf(value) < 0) return "usage: span 2m|1h|24h|7d"
      service.saveConfig(Object.assign({}, service.config, { span: value }))
      return "ok"
    }
    function metrics(): string { return JSON.stringify(BarModel.metrics(service.snapshot)) }
    function ping(): string { return "ok" }
  }
}
