// Run with: node --test tests/
const test = require("node:test")
const assert = require("node:assert/strict")
const Settings = require("./load.js").load("Settings.js")

test("missing settings fall back to the defaults", () => {
  assert.deepEqual(Settings.normalizeConfig(null),
    { interval: 2, units: "c", cards: ["cpu", "memory", "gpu", "storage", "network"], span: "2m", panelWidth: 0, panelHeight: 0, panelX: 0, panelY: 0 })
})

test("the interval stays between 1 and 60 seconds, rounded", () => {
  assert.equal(Settings.normalizeConfig({ interval: 0 }).interval, 2)
  assert.equal(Settings.normalizeConfig({ interval: 61 }).interval, 2)
  assert.equal(Settings.normalizeConfig({ interval: 5.4 }).interval, 5)
  assert.equal(Settings.normalizeConfig({ interval: "x" }).interval, 2)
})

test("unknown and repeated cards are dropped", () => {
  assert.deepEqual(Settings.normalizeConfig({ cards: ["gpu", "nope", "gpu", "cpu"] }).cards, ["gpu", "cpu"])
  assert.deepEqual(Settings.normalizeConfig({ cards: [] }).cards, [])
})

test("units and span accept only their exact values", () => {
  assert.equal(Settings.normalizeConfig({ units: "F" }).units, "c")
  assert.equal(Settings.normalizeConfig({ units: "f" }).units, "f")
  assert.equal(Settings.normalizeConfig({ span: "7d" }).span, "7d")
  assert.equal(Settings.normalizeConfig({ span: "1y" }).span, "2m")
})

test("languageFromEnv reads $LANG", () => {
  assert.equal(Settings.languageFromEnv("es_AR.UTF-8"), "es")
  assert.equal(Settings.languageFromEnv(""), "en")
  assert.equal(Settings.languageFromEnv(undefined), "en")
})

// ---- the Settings rows of the panel

const SNAP = { schema: 1, cpu: { usage_pct: 5, temp_c: 40, load: [1, 1, 1] }, memory: { used_pct: 20 },
               gpus: [{ name: "RTX", usage_pct: 3, temp_c: 40 }],
               self: { avg_ms: 4.9, rss_b: 14 * 1024 * 1024, interval_s: 2 } }
const CFG = Settings.normalizeConfig({})
const T = (s, a) => s.replace(/%(\d+)/g, (m, n) => a[n - 1])
const row = (rows, id) => rows.find(r => r.id === id)
const apply = (id, key, bar, cfg, lang) => {
  const rows = Settings.rows(bar || {}, cfg || CFG, lang || "en", SNAP, T)
  return Settings.apply(row(rows, id), key, bar || {}, cfg || CFG, lang || "en", SNAP)
}

test("rows start with the shown metrics, then the hidden ones, then the rest", () => {
  const rows = Settings.rows({}, CFG, "en", SNAP, T)
  assert.deepEqual(rows.slice(0, 7).map(r => r.id),
                   ["look", "metric:cpu", "metric:cputemp", "metric:ram", "metric:gpu", "metric:gputemp", "metric:vram"])
  assert.equal(row(rows, "metric:cpu").value, "shown")
  assert.equal(row(rows, "metric:vram").value, "hidden")
  assert.deepEqual(rows.filter(r => r.kind === "card").slice(0, 5).map(r => r.id),
                   ["card:cpu", "card:memory", "card:gpu", "card:storage", "card:network"])
  assert.deepEqual(rows.slice(-6).map(r => r.id), ["units", "interval", "span", "panel", "language", "cost"])
  assert.equal(row(rows, "cost").value, "4.9 ms per sample · 14.0M memory · every 2 s")
  assert.equal(row(Settings.rows({}, CFG, "en", null, T), "cost").value, "—")
  assert.match(row(rows, "metric:cputemp").detail, /threshold 85/)
})

const ids = (bar, cfg) => Settings.rows(bar, cfg || CFG, "en", SNAP, T).map(r => r.id)
const merged = (bar, change) => Object.assign({}, bar, change)

// Clicking a row must not move it: the user would click the wrong one next.
test("Space shows or hides a metric without moving any row", () => {
  const before = ids({})
  const on = apply("metric:vram", " ").bar
  assert.deepEqual(on.show, ["cpu", "cputemp", "ram", "gpu", "gputemp", "vram"])
  assert.deepEqual(ids(merged({}, on)), before)
  const off = apply("metric:cpu", " ", merged({}, on)).bar
  assert.deepEqual(off.show, ["cputemp", "ram", "gpu", "gputemp", "vram"])
  assert.deepEqual(ids(merged(merged({}, on), off)), before)
  // Shown again, cpu comes back at its row's place, first on the bar.
  const back = apply("metric:cpu", " ", merged(merged({}, on), off)).bar
  assert.deepEqual(back.show[0], "cpu")
})

test("K and J move a row, shown or hidden, and the bar follows", () => {
  const up = apply("metric:cputemp", "K").bar
  assert.deepEqual(up.order.slice(0, 2), ["cputemp", "cpu"])
  assert.deepEqual(up.show.slice(0, 2), ["cputemp", "cpu"])
  assert.deepEqual(ids(merged({}, up)).slice(1, 3), ["metric:cputemp", "metric:cpu"])
  assert.deepEqual(apply("metric:cpu", "K").bar.order.slice(0, 2), ["cpu", "cputemp"])
  const hidden = apply("metric:vram", "K").bar
  assert.deepEqual(hidden.order.slice(4, 6), ["vram", "gputemp"])
  assert.deepEqual(hidden.show, ["cpu", "cputemp", "ram", "gpu", "gputemp"])
})

test("a saved order with unknown or missing keys is repaired", () => {
  const order = ids({ order: ["ram", "bogus", "ram", "cpu"], show: ["cpu"] }).filter(i => i.startsWith("metric:"))
  assert.deepEqual(order.slice(0, 3), ["metric:ram", "metric:cpu", "metric:cputemp"])
  assert.equal(order.length, 12)
})

test("s cycles the style, o toggles only-above-threshold", () => {
  assert.deepEqual(apply("metric:cpu", "s", { styles: { cpu: "value" } }).bar, { styles: { cpu: "spark" } })
  assert.deepEqual(apply("metric:cpu", "s", { styles: { cpu: "gauge" } }).bar.styles, { cpu: "icon" })
  assert.deepEqual(apply("metric:cpu", "o").bar, { onlyOver: ["cpu"] })
  assert.deepEqual(apply("metric:cpu", "o", { onlyOver: ["cpu", "ram"] }).bar.onlyOver, ["ram"])
})

test("h and l move the threshold by 5, within its range", () => {
  assert.deepEqual(apply("metric:cputemp", "l").bar, { thresholds: { cputemp: 90 } })
  assert.equal(apply("metric:cputemp", "l", { thresholds: { cputemp: 110 } }).bar.thresholds.cputemp, 110)
  assert.equal(apply("metric:cputemp", "h", { thresholds: { cputemp: 50 } }).bar.thresholds.cputemp, 50)
  assert.equal(apply("metric:cpu", "h", { thresholds: { cpu: 0 } }).bar.thresholds.cpu, 0)
  assert.equal(apply("metric:net", "l").bar, null) // no threshold to move
})

test("cards, units, interval, span, icon and language", () => {
  assert.deepEqual(apply("card:load", " ").config.cards, ["cpu", "memory", "gpu", "storage", "network", "load"])
  assert.deepEqual(apply("card:memory", "K").config.cards.slice(0, 2), ["memory", "cpu"])
  assert.deepEqual(apply("card:memory", "K").config.cardOrder.slice(0, 2), ["memory", "cpu"])
  assert.equal(apply("units", " ").config.units, "f")
  assert.equal(apply("interval", "h", {}, Settings.normalizeConfig({ interval: 1 })).config.interval, 1)
  assert.equal(apply("interval", "l").config.interval, 3)
  assert.equal(apply("span", "l").config.span, "1h")
  assert.equal(apply("span", "h").config.span, "7d")
  assert.deepEqual(apply("icon", " ").bar, { showIcon: false })
  assert.deepEqual(apply("language", " "), { bar: null, config: null, language: "es" })
})

test("an unknown key changes nothing", () => {
  for (const id of ["metric:cpu", "card:cpu", "units", "interval", "cost"])
    assert.deepEqual(apply(id, "z"), { bar: null, config: null, language: null }, id)
})

test("Space on a card does not move any row", () => {
  const before = ids({})
  const off = Settings.normalizeConfig(apply("card:cpu", " ").config)
  assert.deepEqual(off.cards, ["memory", "gpu", "storage", "network"])
  assert.deepEqual(ids({}, off), before)
  assert.deepEqual(apply("card:cpu", " ", {}, off).config.cards[0], "cpu")
  assert.equal(Settings.normalizeConfig({}).cardOrder, undefined)
})

// The list is updated in place while the rows are the same ones, so a tick
// (the cost row changes every sample) does not scroll it back to the top.
test("sameRows: same ids in the same order", () => {
  const a = Settings.rows({}, CFG, "en", SNAP, T)
  const b = Settings.rows({}, CFG, "en", Object.assign({}, SNAP, { self: { avg_ms: 5.1, rss_b: 1, interval_s: 2 } }), T)
  assert.equal(Settings.sameRows(a, b), true)
  assert.equal(Settings.sameRows(a, Settings.rows(apply("metric:cputemp", "K").bar, CFG, "en", SNAP, T)), false)
  assert.equal(Settings.sameRows(a, a.slice(1)), false)
  assert.equal(Settings.sameRows([], []), true)
})

test("o needs a threshold: without one it would hide the metric for good", () => {
  assert.equal(apply("metric:net", "o").bar, null)
  assert.equal(apply("metric:load", "o").bar, null)
  assert.deepEqual(apply("metric:cpu", "o").bar, { onlyOver: ["cpu"] })
})

test("temperature thresholds are shown in the bar's units", () => {
  const rows = Settings.rows({}, Settings.normalizeConfig({ units: "f" }), "en", SNAP, T)
  assert.match(row(rows, "metric:cputemp").detail, /threshold 185 °F/)
  assert.match(row(rows, "metric:cpu").detail, /threshold 90$/)
  assert.match(row(Settings.rows({}, CFG, "en", SNAP, T), "metric:cputemp").detail, /threshold 85 °C/)
})

test("the last section is kept in the config", () => {
  assert.equal(Settings.normalizeConfig({ section: "cpu" }).section, "cpu")
  assert.equal(Settings.normalizeConfig({ section: 5 }).section, undefined)
  assert.equal(Settings.normalizeConfig({}).section, undefined)
})

test("the bar look row comes first in the bar group and toggles", () => {
  const cfg = Settings.normalizeConfig(null)
  const rows = Settings.rows({}, cfg, "en", null, s => s)
  const bar = rows.filter(r => r.group === "Bar")
  assert.equal(bar[0].id, "look")
  assert.equal(bar[0].value, "Detailed")
  const r = Settings.apply(bar[0], " ", {}, cfg, "en", null)
  assert.deepEqual(r.bar, { look: "minimal" })
  const back = Settings.apply(bar[0], " ", { look: "minimal" }, cfg, "en", null)
  assert.deepEqual(back.bar, { look: "detailed" })
})

test("a saved order that is a Qt sequence is honoured", () => {
  const o = { length: 2, 0: "ram", 1: "cpu" }
  const rows = Settings.rows({ order: o, show: ["cpu", "ram"] }, CFG, "en", SNAP, T)
  assert.deepEqual(rows.filter(r => r.kind === "metric").slice(0, 2).map(r => r.id), ["metric:ram", "metric:cpu"])
})

test("an onlyOver list that is a Qt sequence shows in the row and toggles", () => {
  const bar = { onlyOver: { length: 1, 0: "cpu" } }
  const rows = Settings.rows(bar, CFG, "en", SNAP, T)
  assert.match(row(rows, "metric:cpu").detail, /only above threshold/)
  assert.deepEqual(Settings.apply(row(rows, "metric:cpu"), "o", bar, CFG, "en", SNAP).bar, { onlyOver: [] })
})

// ---- the panel's size and place (dragged in the panel, kept in config.json)

test("the panel geometry is kept when valid and dropped when not", () => {
  const c = Settings.normalizeConfig({ panelWidth: 1000, panelHeight: 700.4, panelX: -40, panelY: 12 })
  assert.deepEqual([c.panelWidth, c.panelHeight, c.panelX, c.panelY], [1000, 700, -40, 12])
  const bad = Settings.normalizeConfig({ panelWidth: "big", panelHeight: -5, panelX: NaN, panelY: 1e9 })
  assert.deepEqual([bad.panelWidth, bad.panelHeight, bad.panelX, bad.panelY], [0, 0, 0, 4000])
  const none = Settings.normalizeConfig({})
  assert.deepEqual([none.panelWidth, none.panelHeight, none.panelX, none.panelY], [0, 0, 0, 0])
})

test("the panel size row shows the size and Space puts it back to automatic", () => {
  const cfg = Settings.normalizeConfig({ panelWidth: 1000, panelHeight: 700, panelX: 30 })
  const r = row(Settings.rows({}, cfg, "en", SNAP, T), "panel")
  assert.equal(r.value, "1000 × 700")
  const out = Settings.apply(r, " ", {}, cfg, "en", SNAP).config
  assert.deepEqual([out.panelWidth, out.panelHeight, out.panelX, out.panelY], [0, 0, 0, 0])
  assert.equal(row(Settings.rows({}, Settings.normalizeConfig({}), "en", SNAP, T), "panel").value, "automatic")
})
