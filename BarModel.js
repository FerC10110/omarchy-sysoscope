.import "Format.js" as Format

// The bar's metrics: where each one comes from in a snapshot, the short
// history the service keeps for the sparklines, and the text of each segment.
// No QML here, so node can test it (tests/barmodel.test.js loads it with
// tests/load.js, which resolves the .import line).

// key → { icon, tag, section, history, threshold, below?, kind, label }. history is
// the collector's metric name (sysoscope history --metric), threshold the
// default urgent level, and below marks a metric that is urgent when low.
var METRICS = {
  cpu:     { icon: "\u{F061A}", tag: "CPU", section: "cpu",     history: "cpu.usage",    threshold: 90,   kind: "pct",  label: "CPU" },
  cputemp: { icon: "\u{F050F}", tag: "CPU", section: "cpu",     history: "cpu.temp",     threshold: 85,   kind: "temp", label: "CPU temperature" },
  ram:     { icon: "\u{F035B}", tag: "MEM", section: "memory",  history: "mem.used_pct", threshold: 90,   kind: "pct",  label: "Memory" },
  gpu:     { icon: "\u{F08AE}", tag: "GPU", section: "gpu",     history: "gpu.usage",    threshold: 95,   kind: "pct",  label: "GPU" },
  gputemp: { icon: "\u{F050F}", tag: "GPU", section: "gpu",     history: "gpu.temp",     threshold: 85,   kind: "temp", label: "GPU temperature" },
  vram:    { icon: "\u{F08AE}", tag: "VRM", section: "gpu",     history: "gpu.vram_pct", threshold: 90,   kind: "pct",  label: "VRAM" },
  disk:    { icon: "\u{F02CA}", tag: "DSK", section: "storage", history: null,           threshold: 90,   kind: "pct",  label: "Disk" },
  diskio:  { icon: "\u{F04E1}", tag: "I/O", section: "storage", history: "disk.read",    threshold: null, kind: "rate", label: "Disk activity" },
  net:     { icon: "\u{F0317}", tag: "NET", section: "network", history: "net.rx",       threshold: null, kind: "rate", label: "Network" },
  load:    { icon: "\u{F029A}", tag: "LOA", section: "cpu",     history: "load1",        threshold: null, kind: "load", label: "Load" },
  battery: { icon: "\u{F0079}", tag: null, section: "power",   history: "bat.pct",      threshold: 15, below: true, kind: "pct", label: "Battery" },
  profile: { icon: "\u{F0241}", tag: null, section: "power",   history: null,           threshold: null, kind: "text", label: "Power profile" }
}
var ORDER = ["cpu", "cputemp", "ram", "gpu", "gputemp", "vram", "disk", "diskio", "net", "load", "battery", "profile"]
var STYLES = ["icon", "value", "spark", "gauge"]
var PLUGIN_ICON = "\u{F0208}"
var RECENT_MAX = 60
var LOOKS = ["minimal", "detailed"]

// The bar's look: "minimal" is the icon-and-value bar; anything else,
// including a missing or hand-edited value, is the detailed one.
function look(settings) { return settings && settings.look === "minimal" ? "minimal" : "detailed" }

function num(v) { return (typeof v === "number" && isFinite(v)) ? v : null }

// The first awake GPU that has field f, or null.
function gpuWith(snap, f) {
  var gs = (snap && snap.gpus) || []
  for (var i = 0; i < gs.length; i++)
    if (!gs[i].suspended && num(gs[i][f]) !== null) return gs[i]
  return null
}

// A metric's current value: a number (text for profile) or null.
function valueOf(key, snap) {
  if (!snap) return null
  var c = snap.cpu || {}, g
  switch (key) {
  case "cpu": return num(c.usage_pct)
  case "cputemp": return num(c.temp_c)
  case "ram": return snap.memory ? num(snap.memory.used_pct) : null
  case "gpu": g = gpuWith(snap, "usage_pct"); return g ? g.usage_pct : null
  case "gputemp": g = gpuWith(snap, "temp_c"); return g ? g.temp_c : null
  case "vram":
    g = gpuWith(snap, "vram_total_b")
    return (g && num(g.vram_used_b) !== null && g.vram_total_b > 0) ? g.vram_used_b / g.vram_total_b * 100 : null
  case "disk":
    var fs = (snap.storage && snap.storage.filesystems) || []
    for (var i = 0; i < fs.length; i++)
      if ((fs[i].mounts || []).indexOf("/") >= 0) return num(fs[i].used_pct)
    return null
  case "diskio":
    var ds = (snap.storage && snap.storage.disks) || [], sum = null
    for (var j = 0; j < ds.length; j++) {
      var r = num(ds[j].read_bps), w = num(ds[j].write_bps)
      if (r !== null || w !== null) sum = (sum || 0) + (r || 0) + (w || 0)
    }
    return sum
  case "net":
    // The totals read 0 until a second sample gives a rate; the interfaces
    // leave theirs out until then.
    var ifs = (snap.network && snap.network.interfaces) || []
    if (!ifs.some(function(it) { return num(it.rx_bps) !== null })) return null
    return (num(snap.network.rx_bps) || 0) + (num(snap.network.tx_bps) || 0)
  case "load": return (c.load && num(c.load[0]) !== null) ? c.load[0] : null
  case "battery":
    var bs = (snap.power && snap.power.batteries) || []
    for (var k = 0; k < bs.length; k++)
      if (!bs[k].peripheral) return num(bs[k].pct)
    return null
  case "profile": return (snap.power && snap.power.profile) || null
  }
  return null
}

// { key: value } of every metric, for the IPC.
function metrics(snap) {
  var out = {}
  ORDER.forEach(function(k) { out[k] = valueOf(k, snap) })
  return out
}

// A new recent map with this snapshot's numeric values appended (up to
// RECENT_MAX per metric). The input is not modified.
function push(recent, snap) {
  var out = {}
  for (var k in recent) out[k] = recent[k]
  ORDER.forEach(function(key) {
    var v = valueOf(key, snap)
    if (typeof v !== "number") return
    var list = (out[key] || []).concat([v])
    out[key] = list.length > RECENT_MAX ? list.slice(list.length - RECENT_MAX) : list
  })
  return out
}

// What a new bar shows: CPU, its temperature and memory, the GPU when there
// is one, and the battery on a laptop.
function defaultShow(snap) {
  var out = ["cpu", "cputemp", "ram"]
  if (snap && (snap.gpus || []).length > 0) out.push("gpu", "gputemp")
  if (valueOf("battery", snap) !== null) out.push("battery")
  return out
}

// A list from shell.json as a real Array, or null. After a shell restart Qt
// hands those lists over as sequences (indexable, with a length) that
// Array.isArray rejects.
function toList(x) {
  if (Array.isArray(x)) return x
  if (!x || typeof x !== "object" || typeof x.length !== "number") return null
  return Array.prototype.slice.call(x)
}

function normalizeShow(list, snap) {
  list = toList(list)
  if (!list) return defaultShow(snap)
  return list.filter(function(k, i) { return METRICS.hasOwnProperty(k) && list.indexOf(k) === i })
}

// The user's threshold for key, or the default; below: urgent when low.
function isUrgent(key, value, thresholds) {
  var m = METRICS[key]
  if (!m || typeof value !== "number") return false
  var th = (thresholds && typeof thresholds[key] === "number") ? thresholds[key] : m.threshold
  if (typeof th !== "number") return false
  return m.below ? value <= th : value >= th
}

var PROFILES = { "power-saver": "save", "balanced": "bal", "performance": "perf" }

function formatValue(key, v, units) {
  switch (METRICS[key].kind) {
  case "pct": return Format.pct(v)
  case "temp": return Format.temp(v, units)
  case "rate": return Format.rate(v)
  case "load": return typeof v === "number" ? v.toFixed(2) : "—"
  }
  return v === null ? "—" : (PROFILES[v] || String(v))
}

function sparkOf(key, values) {
  var k = METRICS[key].kind
  return Format.spark(values, 0, (k === "pct" || k === "temp") ? 100 : null)
}

// Readings next to a metric for its tooltip: CPU temperature and clock, the
// memory in bytes, GPU temperature and power, the upload next to the download.
function related(key, snap, units, t) {
  if (!snap) return ""
  var c = snap.cpu || {}, g, parts = []
  switch (key) {
  case "cpu":
    if (num(c.temp_c) !== null) parts.push(formatValue("cputemp", c.temp_c, units))
    if (num(c.avg_mhz) !== null) parts.push(Format.mhz(c.avg_mhz))
    break
  case "ram":
    var m = snap.memory
    if (m) parts.push(t("%1 of %2", [Format.bytes(m.used_b), Format.bytes(m.total_b)]))
    break
  case "gpu":
    g = gpuWith(snap, "usage_pct")
    if (g && num(g.temp_c) !== null) parts.push(formatValue("gputemp", g.temp_c, units))
    if (g && num(g.power_w) !== null) parts.push(g.power_w.toFixed(0) + " W")
    break
  case "net":
    if (valueOf("net", snap) !== null) parts.push("↑ " + Format.rate(snap.network.tx_bps))
    break
  }
  return parts.join(" · ")
}

// The frozen apps (Sysoscope resumes each one after 5 minutes): a snowflake
// with how many, opening the Applications section. null when there are none.
function frozenSegment(snap, nowMs, t) {
  t = t || function(s, args) { return s.replace(/%(\d+)/g, function(m, n) { return args[n - 1] }) }
  var list = snap && snap.apps && Array.isArray(snap.apps.frozen) ? snap.apps.frozen : []
  if (list.length === 0) return null
  var names = list.map(function(f) {
    var min = Math.max(0, Math.ceil((Date.parse(f.until) - nowMs) / 60000))
    return t("%1 (%2 min)", [f.name, min])
  })
  return { key: "frozen", text: "❄ " + list.length, urgent: false, section: "apps",
           tooltip: t("Frozen: %1", [names.join(", ")]), style: "icon", icon: "❄",
           value: String(list.length), frac: null, series: [], sub: "" }
}

// The bar's segments: [{ key, text, urgent, section, tooltip, style, tag,
// icon, value, frac, series, sub }]; text is the minimal bar's, the rest the detailed one's. settings:
// { show, styles, onlyOver, thresholds, showIcon }. t translates (identity
// when absent). nowMs: the clock for the frozen apps' minutes, Date.now() when absent.
function segments(snap, settings, recent, units, t, nowMs) {
  settings = settings || {}
  recent = recent || {}
  t = t || function(s, args) { return s.replace(/%(\d+)/g, function(m, n) { return args[n - 1] }) }
  var out = []
  if (settings.showIcon !== false)
    out.push({ key: "icon", text: PLUGIN_ICON, urgent: false, section: "summary", tooltip: "" })
  var fz = frozenSegment(snap, nowMs || Date.now(), t)
  if (fz) out.push(fz)
  var onlyOver = toList(settings.onlyOver) || []
  normalizeShow(settings.show, snap).forEach(function(key) {
    var m = METRICS[key], v = valueOf(key, snap)
    var urgent = isUrgent(key, v, settings.thresholds)
    if (onlyOver.indexOf(key) >= 0 && !urgent) return
    var value = formatValue(key, v, units)
    var style = STYLES.indexOf((settings.styles || {})[key]) >= 0 ? settings.styles[key] : "value"
    var text = m.icon
    if (style === "value") text += " " + value
    else if (style === "spark") text += " " + value + " " + sparkOf(key, recent[key])
    else if (style === "gauge") text += Format.gauge(typeof v === "number" ? v / 100 : null)
    var nums = (recent[key] || []).filter(function(x) { return typeof x === "number" })
    var tooltip = t(m.label) + ": " + value
    if (nums.length > 0 && m.kind !== "text")
      tooltip = t("%1: %2 · peak %3 · %4", [t(m.label), value, formatValue(key, Math.max.apply(null, nums), units), sparkOf(key, nums)])
    var rel = related(key, snap, units, t)
    if (rel) tooltip += " · " + rel
    var frac = (typeof v === "number" && (m.kind === "pct" || m.kind === "temp")) ? Math.max(0, Math.min(1, v / 100)) : null
    // The detailed bar stacks the upload under the download.
    var sub = key === "net" && v !== null ? "↑ " + Format.rate(snap.network.tx_bps) : ""
    out.push({ key: key, text: text, urgent: urgent, section: m.section, tooltip: tooltip,
               style: style, icon: m.icon, tag: m.tag, value: value, frac: frac, series: nums, sub: sub })
  })
  // Nothing to draw would leave a widget nobody can click to open the panel.
  if (out.length === 0) out.push({ key: "icon", text: PLUGIN_ICON, urgent: false, section: "summary", tooltip: "" })
  return out
}

// Argus's pattern for updateEntryInline, which replaces the whole entry: a
// copy of the settings with one of them changed.
// changes is { name: value, … }: settings that change together go in one
// write, since the next write would start from the entry before this one.
function mergeEntry(moduleName, settings, changes) {
  var out = { id: moduleName }
  for (var k in settings || {})
    if (k !== "id" && !(k in changes)) out[k] = settings[k]
  for (var c in changes) out[c] = changes[c]
  return out
}

// One widget per monitor; only the first one answers the IPC.
function isLeader(widgets, me) { return !!widgets && widgets.length > 0 && widgets[0] === me }

// The copy an IPC open/toggle acts on: the host's own choice (the open copy,
// then the focused monitor's), or the first copy on a host without it.
function panelTarget(bar, moduleName) {
  if (!bar) return null
  if (typeof bar.findPanelWidget === "function") return bar.findPanelWidget(moduleName)
  var widgets = bar.moduleWidgets ? bar.moduleWidgets(moduleName) : []
  return widgets && widgets.length > 0 ? widgets[0] : null
}

if (typeof module !== "undefined") {
  module.exports = { toList: toList, LOOKS: LOOKS, look: look, related: related, METRICS: METRICS, ORDER: ORDER, STYLES: STYLES, PLUGIN_ICON: PLUGIN_ICON,
                     valueOf: valueOf, metrics: metrics, push: push, defaultShow: defaultShow,
                     normalizeShow: normalizeShow, isUrgent: isUrgent, segments: segments,
                     frozenSegment: frozenSegment,
                     mergeEntry: mergeEntry, isLeader: isLeader, panelTarget: panelTarget }
}
