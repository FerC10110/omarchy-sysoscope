.import "BarModel.js" as BarModel
.import "Format.js" as Format

// Normalizes the service settings kept in $XDG_CONFIG_HOME/sysoscope/config.json.
// Anything missing or out of range falls back to its default.

var CARDS = ["cpu", "cputemp", "memory", "gpu", "gputemp", "storage", "network", "power", "load"]
var DEFAULT_CARDS = ["cpu", "memory", "gpu", "storage", "network"]
var SPANS = ["2m", "1h", "24h", "7d"]

function normalizeConfig(raw) {
  var c = (raw && typeof raw === "object") ? raw : {}
  var interval = Number(c.interval)
  var cards = Array.isArray(c.cards) ? c.cards.filter(function(k, i, a) {
    return CARDS.indexOf(k) >= 0 && a.indexOf(k) === i
  }) : DEFAULT_CARDS.slice()
  var out = {
    interval: (isFinite(interval) && interval >= 1 && interval <= 60) ? Math.round(interval) : 2,
    units: c.units === "f" ? "f" : "c",
    cards: cards,
    span: SPANS.indexOf(c.span) >= 0 ? c.span : "2m"
  }
  if (Array.isArray(c.cardOrder)) out.cardOrder = ordered(c.cardOrder, CARDS, cards)
  if (typeof c.section === "string" && c.section !== "") out.section = c.section
  // The panel's size and place, dragged in the panel; 0 is automatic.
  out.panelWidth = pixels(c.panelWidth, 0, 4000)
  out.panelHeight = pixels(c.panelHeight, 0, 3000)
  out.panelX = pixels(c.panelX, -4000, 4000)
  out.panelY = pixels(c.panelY, -4000, 4000)
  return out
}

function pixels(v, lo, hi) {
  var n = Number(v)
  if (typeof v !== "number" || !isFinite(n) || n < lo) return 0
  return Math.round(Math.min(hi, n))
}

// $LANG → "es" or "en" (used when plugin-language.json does not exist).
function languageFromEnv(lang) { return /^es/i.test(String(lang || "")) ? "es" : "en" }

// ---- the Settings rows of the panel

// The Summary cards as the collector names them (sysoscope view summary).
var CARD_LABELS = {
  cpu: { label: "CPU" }, cputemp: { label: "CPU temperature" }, memory: { label: "Memory" },
  gpu: { label: "GPU" }, gputemp: { label: "GPU temperature" }, storage: { label: "Disk /" },
  network: { label: "Network" }, power: { label: "Power" }, load: { label: "Load" }
}

function styleName(style, t) {
  switch (style) {
  case "icon": return t("icon")
  case "spark": return t("sparkline")
  case "gauge": return t("gauge")
  }
  return t("value")
}

function thresholdOf(key, bar) {
  var th = bar.thresholds || {}
  return typeof th[key] === "number" ? th[key] : BarModel.METRICS[key].threshold
}

// Temperature thresholds are kept in °C and shown in the bar's units.
function thresholdText(key, th, units) {
  if (BarModel.METRICS[key].kind !== "temp") return String(th)
  return units === "f" ? Math.round(th * 9 / 5 + 32) + " °F" : th + " °C"
}

// Shown ones first, in their order, then the rest in `all` order.
function shownFirst(shown, all) {
  return shown.concat(all.filter(function(k) { return shown.indexOf(k) < 0 }))
}

// The rows' order: the saved one (repaired: unknown and repeated keys out,
// missing ones at the end), or the shown items first until one is saved.
// Showing or hiding never changes it, so a row stays under the pointer.
function ordered(saved, all, shown) {
  saved = BarModel.toList(saved)
  if (!saved) return shownFirst(shown, all)
  var out = saved.filter(function(k, i) { return all.indexOf(k) >= 0 && saved.indexOf(k) === i })
  return out.concat(all.filter(function(k) { return out.indexOf(k) < 0 }))
}

function only(order, set) { return order.filter(function(k) { return set.indexOf(k) >= 0 }) }

// One row per setting: { id, kind, group, label, value, detail }. kind is
// metric | card | choice | number | info. bar is the widget's settings (from
// shell.json), config the service's (config.json).
function rows(bar, config, language, snap, t) {
  bar = bar || {}
  var out = []
  var shown = BarModel.normalizeShow(bar.show, snap)
  out.push({ id: "look", kind: "choice", group: t("Bar"), label: t("Bar look"),
             value: BarModel.look(bar) === "minimal" ? t("Minimal") : t("Detailed"), detail: "" })
  ordered(bar.order, BarModel.ORDER, shown).forEach(function(k) {
    var m = BarModel.METRICS[k], detail = [styleName((bar.styles || {})[k], t)]
    var th = thresholdOf(k, bar)
    if (typeof th === "number") detail.push(t("threshold %1", [thresholdText(k, th, config.units)]))
    if ((BarModel.toList(bar.onlyOver) || []).indexOf(k) >= 0) detail.push(t("only above threshold"))
    out.push({ id: "metric:" + k, kind: "metric", group: t("Bar"), label: t(m.label),
               value: shown.indexOf(k) >= 0 ? t("shown") : t("hidden"), detail: detail.join(" · ") })
  })
  out.push({ id: "icon", kind: "choice", group: t("Bar"), label: t("Show the Sysoscope icon"),
             value: bar.showIcon === false ? t("off") : t("on"), detail: "" })
  ordered(config.cardOrder, CARDS, config.cards).forEach(function(k) {
    out.push({ id: "card:" + k, kind: "card", group: t("Summary cards"), label: t(CARD_LABELS[k].label),
               value: config.cards.indexOf(k) >= 0 ? t("shown") : t("hidden"), detail: "" })
  })
  var self = snap && snap.self
  out.push(
    { id: "units", kind: "choice", group: t("General"), label: t("Temperature"), value: config.units === "f" ? "°F" : "°C", detail: "" },
    { id: "interval", kind: "number", group: t("General"), label: t("Refresh every"), value: t("%1 s", [config.interval]), detail: "" },
    { id: "span", kind: "choice", group: t("General"), label: t("Chart period"), value: config.span, detail: "" },
    { id: "panel", kind: "choice", group: t("General"), label: t("Panel size"),
      value: config.panelWidth > 0 ? config.panelWidth + " × " + config.panelHeight : t("automatic"),
      detail: t("drag its corner or its title; Space resets") },
    { id: "language", kind: "choice", group: t("General"), label: t("Language"), value: language === "es" ? "Español" : "English",
      detail: t("shared by every plugin") },
    { id: "cost", kind: "info", group: t("General"), label: t("Collector cost"),
      value: self ? t("%1 ms per sample · %2 memory · every %3 s", [self.avg_ms, Format.bytes(self.rss_b), self.interval_s]) : "—",
      detail: "" })
  return out
}

// True when both lists hold the same rows in the same order (their values
// may differ): the view then updates them in place and keeps its scroll.
function sameRows(a, b) {
  if (a.length !== b.length) return false
  for (var i = 0; i < a.length; i++) if (a[i].id !== b[i].id) return false
  return true
}

function moved(list, item, dir) {
  var i = list.indexOf(item), j = i + dir
  if (i < 0 || j < 0 || j >= list.length) return list.slice()
  var out = list.slice()
  out[i] = list[j]
  out[j] = item
  return out
}

function toggled(list, item) {
  return list.indexOf(item) >= 0 ? list.filter(function(k) { return k !== item }) : list.concat([item])
}

function copy(o) {
  var out = {}
  for (var k in o || {}) out[k] = o[k]
  return out
}

// What a key does on a row: { bar: {name, value} | null, config: cfg | null,
// language: lang | null }. Keys: " " (Space/Enter), "h"/"l" (left/right), "K"/"J"
// (move up/down), "s" (style), "o" (only above threshold). snap gives the
// default bar metrics when the user never chose any.
function apply(row, key, bar, config, language, snap) {
  var none = { bar: null, config: null, language: null }
  if (!row) return none
  bar = bar || {}
  var out = { bar: null, config: null, language: null }
  var cfg = function(change) { var c = copy(config); for (var k in change) c[k] = change[k]; out.config = normalizeConfig(c); return out }
  var parts = row.id.split(":"), id = parts[0], k = parts[1]
  var shown = BarModel.normalizeShow(bar.show, snap)
  if (id === "metric") {
    var order = ordered(bar.order, BarModel.ORDER, shown)
    if (key === " ") { out.bar = { show: only(order, toggled(shown, k)), order: order }; return out }
    if (key === "K" || key === "J") {
      order = moved(order, k, key === "K" ? -1 : 1)
      out.bar = { show: only(order, shown), order: order }
      return out
    }
    if (key === "s") {
      var styles = copy(bar.styles), st = BarModel.STYLES, cur = st.indexOf(styles[k])
      styles[k] = st[((cur < 0 ? st.indexOf("value") : cur) + 1) % st.length]
      out.bar = { styles: styles }
      return out
    }
    if (key === "o") {
      // Without a threshold "only above it" would hide the metric for good.
      if (typeof thresholdOf(k, bar) !== "number") return none
      out.bar = { onlyOver: toggled(BarModel.toList(bar.onlyOver) || [], k) }
      return out
    }
    if (key === "h" || key === "l") {
      var th = thresholdOf(k, bar)
      if (typeof th !== "number") return none
      var temp = BarModel.METRICS[k].kind === "temp"
      var ths = copy(bar.thresholds)
      ths[k] = Math.min(temp ? 110 : 100, Math.max(temp ? 50 : 0, th + (key === "l" ? 5 : -5)))
      out.bar = { thresholds: ths }
      return out
    }
    return none
  }
  if (id === "card") {
    var cards = ordered(config.cardOrder, CARDS, config.cards)
    if (key === " ") return cfg({ cards: only(cards, toggled(config.cards, k)), cardOrder: cards })
    if (key === "K" || key === "J") {
      cards = moved(cards, k, key === "K" ? -1 : 1)
      return cfg({ cards: only(cards, config.cards), cardOrder: cards })
    }
    return none
  }
  var step = key === "h" ? -1 : (key === "l" || key === " ") ? 1 : 0
  if (step === 0) return none
  switch (id) {
  case "look": out.bar = { look: BarModel.look(bar) === "minimal" ? "detailed" : "minimal" }; return out
  case "icon": out.bar = { showIcon: bar.showIcon === false }; return out
  case "units": return cfg({ units: config.units === "f" ? "c" : "f" })
  case "interval":
    var n = config.interval + step
    if (key === " " && n > 60) n = 1
    return cfg({ interval: Math.min(60, Math.max(1, n)) })
  case "panel": return key === " " ? cfg({ panelWidth: 0, panelHeight: 0, panelX: 0, panelY: 0 }) : none
  case "span": return cfg({ span: SPANS[(SPANS.indexOf(config.span) + step + SPANS.length) % SPANS.length] })
  case "language": out.language = language === "es" ? "en" : "es"; return out
  }
  return none
}

if (typeof module !== "undefined") {
  module.exports = { CARDS: CARDS, DEFAULT_CARDS: DEFAULT_CARDS, SPANS: SPANS, CARD_LABELS: CARD_LABELS,
                     normalizeConfig: normalizeConfig, languageFromEnv: languageFromEnv, rows: rows, apply: apply, sameRows: sameRows }
}
