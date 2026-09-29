// Run with: node --test tests/
const test = require("node:test")
const assert = require("node:assert/strict")
const BarModel = require("./load.js").load("BarModel.js")

// A desktop with an NVIDIA card, a suspended second GPU and no battery.
const desktop = {
  schema: 1,
  cpu: { usage_pct: 23.4, temp_c: 41.3, load: [1.84, 1.34, 1.2] },
  memory: { used_pct: 38.6 },
  gpus: [
    { name: "asleep", suspended: true, usage_pct: 99 },
    { name: "RTX 3080", suspended: false, usage_pct: 7, temp_c: 45, vram_used_b: 1000, vram_total_b: 4000 }
  ],
  storage: { disks: [{ read_bps: 100, write_bps: 50 }, { read_bps: 10 }],
             filesystems: [{ mounts: ["/boot"], used_pct: 10 }, { mounts: ["/", "/home"], used_pct: 61 }] },
  network: { rx_bps: 2000, tx_bps: 500, interfaces: [{ name: "enp5s0", rx_bps: 2000, tx_bps: 500 }] },
  power: { profile: "balanced", batteries: [{ peripheral: true, pct: 40 }] }
}

// A machine where almost nothing could be read.
const minimal = { schema: 1, cpu: { usage_pct: 5, load: [0.1, 0, 0] }, memory: { used_pct: 20 } }

test("valueOf reads each metric from the snapshot", () => {
  const m = BarModel.metrics(desktop)
  assert.deepEqual(m, { cpu: 23.4, cputemp: 41.3, ram: 38.6, gpu: 7, gputemp: 45, vram: 25, disk: 61,
                        diskio: 160, net: 2500, load: 1.84, battery: null, profile: "balanced" })
})

test("what the machine does not have is null, never 0", () => {
  const m = BarModel.metrics(minimal)
  for (const k of ["cputemp", "gpu", "gputemp", "vram", "disk", "diskio", "net", "battery", "profile"])
    assert.equal(m[k], null, k)
  assert.deepEqual(BarModel.metrics(null).cpu, null)
})

test("push adds a value per metric, caps at 60 and does not mutate", () => {
  const before = { cpu: Array.from({ length: 60 }, (_, i) => i) }
  const after = BarModel.push(before, desktop)
  assert.equal(before.cpu.length, 60)
  assert.equal(before.cpu[59], 59)
  assert.equal(after.cpu.length, 60)
  assert.equal(after.cpu[59], 23.4)
  assert.equal(after.cpu[0], 1)
  assert.deepEqual(after.ram, [38.6])
  assert.equal("cputemp" in BarModel.push({}, minimal), false) // null values are not recorded
})

const withBattery = Object.assign({}, minimal, { power: { batteries: [{ peripheral: false, pct: 10 }] } })
const text = segs => segs.map(s => s.text)
const seg = (segs, key) => segs.find(s => s.key === key)

test("defaultShow depends on what the machine has", () => {
  assert.deepEqual(BarModel.defaultShow(desktop), ["cpu", "cputemp", "ram", "gpu", "gputemp"])
  assert.deepEqual(BarModel.defaultShow(minimal), ["cpu", "cputemp", "ram"])
  assert.deepEqual(BarModel.defaultShow(withBattery), ["cpu", "cputemp", "ram", "battery"])
})

test("normalizeShow keeps known keys once, and falls back to the default", () => {
  assert.deepEqual(BarModel.normalizeShow(["ram", "nope", "ram", "net"], desktop), ["ram", "net"])
  assert.deepEqual(BarModel.normalizeShow([], desktop), [])
  assert.deepEqual(BarModel.normalizeShow("cpu", minimal), ["cpu", "cputemp", "ram"])
})

test("a missing temperature shows a dash, never 0°", () => {
  const segs = BarModel.segments(minimal, { showIcon: false }, {}, "c")
  assert.equal(seg(segs, "cputemp").text, "\u{F050F} —")
  assert.equal(seg(segs, "cputemp").urgent, false)
})

test("the plugin icon goes first and opens the summary", () => {
  const segs = BarModel.segments(minimal, {}, {}, "c")
  assert.deepEqual(segs[0], { key: "icon", text: BarModel.PLUGIN_ICON, urgent: false, section: "summary", tooltip: "" })
})

test("onlyOver hides a metric until it is urgent", () => {
  const hot = t => Object.assign({}, minimal, { cpu: Object.assign({}, minimal.cpu, { temp_c: t }) })
  const s = { showIcon: false, onlyOver: ["cputemp"] }
  assert.equal(seg(BarModel.segments(hot(60), s, {}, "c"), "cputemp"), undefined)
  assert.equal(seg(BarModel.segments(hot(90), s, {}, "c"), "cputemp").urgent, true)
})

test("thresholds: the user's, the default, and below for the battery", () => {
  assert.equal(BarModel.isUrgent("cpu", 60, { cpu: 50 }), true)
  assert.equal(BarModel.isUrgent("cpu", 60, {}), false)
  assert.equal(BarModel.isUrgent("battery", 10, {}), true)
  assert.equal(BarModel.isUrgent("battery", 50, {}), false)
  assert.equal(BarModel.isUrgent("net", 1e9, {}), false)
  assert.equal(BarModel.isUrgent("cpu", null, {}), false)
})

test("styles: icon, value, spark and gauge", () => {
  const full = Object.assign({}, minimal, { cpu: Object.assign({}, minimal.cpu, { usage_pct: 100 }) })
  const s = styles => ({ showIcon: false, show: ["cpu"], styles: { cpu: styles } })
  assert.deepEqual(text(BarModel.segments(full, s("icon"), {}, "c")), ["\u{F061A}"])
  assert.deepEqual(text(BarModel.segments(full, s("value"), {}, "c")), ["\u{F061A} 100%"])
  assert.deepEqual(text(BarModel.segments(full, s("spark"), { cpu: [0, 100] }, "c")), ["\u{F061A} 100% ▁█"])
  assert.match(text(BarModel.segments(full, s("gauge"), {}, "c"))[0], /█$/)
})

test("the power profile uses its short name", () => {
  const p = Object.assign({}, minimal, { power: { profile: "performance" } })
  assert.deepEqual(text(BarModel.segments(p, { showIcon: false, show: ["profile"] }, {}, "c")), ["\u{F0241} perf"])
})

test("the tooltip names the metric, its peak and its sparkline", () => {
  const segs = BarModel.segments(desktop, { showIcon: false, show: ["cpu"] }, { cpu: [10, 50] }, "c",
                                 (s, a) => s.replace(/%(\d)/g, (m, n) => a[n - 1]))
  assert.equal(seg(segs, "cpu").tooltip, "CPU: 23% · peak 50% · ▂▅ · 41°")
  assert.equal(seg(segs, "cpu").section, "cpu")
})

test("mergeEntry copies the settings and sets the changed ones together", () => {
  assert.deepEqual(BarModel.mergeEntry("x", { id: "x", show: ["cpu"], styles: { cpu: "spark" } }, { show: ["ram"], order: ["ram", "cpu"] }),
                   { id: "x", show: ["ram"], order: ["ram", "cpu"], styles: { cpu: "spark" } })
})

test("isLeader: only the first widget", () => {
  const a = {}, b = {}
  assert.equal(BarModel.isLeader([a, b], a), true)
  assert.equal(BarModel.isLeader([a, b], b), false)
  assert.equal(BarModel.isLeader([], a), false)
  assert.equal(BarModel.isLeader(null, a), false)
})

test("panelTarget follows the host's routing (open copy, then focused monitor)", () => {
  const a = { name: "a" }, b = { name: "b" }
  const bar = { findPanelWidget: id => id === "x" ? b : null, moduleWidgets: () => [a, b] }
  assert.equal(BarModel.panelTarget(bar, "x"), b)
  // An older host without findPanelWidget: the first copy.
  assert.equal(BarModel.panelTarget({ moduleWidgets: () => [a, b] }, "x"), a)
  assert.equal(BarModel.panelTarget(null, "x"), null)
})

test("with the icon off and nothing to show, the icon stays so the widget can be clicked", () => {
  const segs = BarModel.segments(desktop, { showIcon: false, show: ["cpu"], onlyOver: ["cpu"] }, {}, "c")
  assert.deepEqual(segs.map(s => s.key), ["icon"])
})

test("net has no value until the collector has measured a rate", () => {
  const first = { network: { rx_bps: 0, tx_bps: 0, interfaces: [{ name: "enp5s0" }] } }
  assert.equal(BarModel.valueOf("net", first), null)
  const later = { network: { rx_bps: 100, tx_bps: 50, interfaces: [{ name: "enp5s0", rx_bps: 100, tx_bps: 50 }] } }
  assert.equal(BarModel.valueOf("net", later), 150)
})

test("look is detailed unless it says minimal", () => {
  assert.equal(BarModel.look({ look: "minimal" }), "minimal")
  assert.equal(BarModel.look({ look: "detailed" }), "detailed")
  assert.equal(BarModel.look({}), "detailed")
  assert.equal(BarModel.look({ look: "fancy" }), "detailed")
  assert.equal(BarModel.look(null), "detailed")
})

test("every metric but battery and profile has a three-letter tag", () => {
  for (const k of BarModel.ORDER) {
    const tag = BarModel.METRICS[k].tag
    if (k === "battery" || k === "profile") assert.equal(tag, null)
    else assert.match(tag, /^[A-Z/]{3}$/)
  }
})

test("segments carry what the detailed bar draws", () => {
  const segs = BarModel.segments(desktop, { show: ["cpu"], styles: { cpu: "spark" } }, { cpu: [1, 2, 3] }, "c")
  const cpu = segs.find(s => s.key === "cpu")
  assert.equal(cpu.style, "spark")
  assert.equal(cpu.tag, "CPU")
  assert.equal(cpu.value, cpu.text.split(" ")[1])
  assert.deepEqual(cpu.series, [1, 2, 3])
  assert.ok(cpu.frac >= 0 && cpu.frac <= 1)
  const net = BarModel.segments(desktop, { show: ["net"] }, {}, "c").find(s => s.key === "net")
  assert.match(net.sub, /^↑ /)
})

test("the tooltip adds related readings", () => {
  const segs = BarModel.segments(desktop, { show: ["cpu"] }, { cpu: [10, 14, 12] }, "c")
  assert.match(segs.find(s => s.key === "cpu").tooltip, / · 41°$/)
})

test("the upload is left out while the network has no rate yet", () => {
  const first = { network: { rx_bps: 0, tx_bps: 0, interfaces: [{ name: "eth0" }] } }
  assert.equal(BarModel.segments(first, { show: ["net"] }, {}, "c").find(s => s.key === "net").sub, "")
})

test("the network tooltip has no upload while there is no rate yet", () => {
  const first = { network: { rx_bps: 0, tx_bps: 0, interfaces: [{ name: "eth0" }] } }
  assert.equal(BarModel.related("net", first, "c", s => s), "")
})

test("each segment carries its icon, whatever the style", () => {
  const laptop = { power: { batteries: [{ pct: 76 }] } }
  const bat = BarModel.segments(laptop, { show: ["battery"], styles: { battery: "gauge" } }, {}, "c").find(s => s.key === "battery")
  assert.equal(bat.icon, BarModel.METRICS.battery.icon)
})

// Qt hands shell.json lists to QML as sequences: indexable, with a length,
// but not Arrays. After a shell restart the settings arrive like that.
function seq(list) {
  const o = { length: list.length }
  list.forEach((v, i) => { o[i] = v })
  return o
}

test("a show list that is a Qt sequence is honoured, not replaced by the default", () => {
  assert.deepEqual(BarModel.normalizeShow(seq(["cpu", "gpu"]), desktop), ["cpu", "gpu"])
  const keys = BarModel.segments(desktop, { showIcon: false, show: seq(["cpu", "gputemp"]) }, {}, "c").map(s => s.key)
  assert.deepEqual(keys, ["cpu", "gputemp"])
  assert.deepEqual(BarModel.normalizeShow("cpu", desktop), BarModel.defaultShow(desktop))
})

test("an onlyOver list that is a Qt sequence still hides quiet metrics", () => {
  const onlyOver = { length: 1, 0: "cpu", indexOf: undefined }
  const keys = BarModel.segments(desktop, { showIcon: false, show: ["cpu", "ram"], onlyOver }, {}, "c").map(s => s.key)
  assert.deepEqual(keys, ["ram"])
})

const NOW = Date.parse("2026-09-24T12:00:00Z")
const frozen = Object.assign({}, minimal, { apps: { list: [], frozen: [
  { id: "comm:make", name: "make", until: "2026-09-24T12:04:10Z" },
  { id: "x.scope", name: "Chromium", until: "2026-09-24T12:00:20Z" }] } })

test("frozenSegment: how many, where to go, and each one's minutes left", () => {
  const s = BarModel.frozenSegment(frozen, NOW)
  assert.equal(s.key, "frozen")
  assert.equal(s.text, "❄ 2")
  assert.equal(s.section, "apps")
  assert.equal(s.style, "icon")
  assert.equal(s.tooltip, "Frozen: make (5 min), Chromium (1 min)")
  assert.equal(BarModel.frozenSegment(minimal, NOW), null)
  assert.equal(BarModel.frozenSegment(null, NOW), null)
})

test("segments puts the frozen apps right after the icon", () => {
  const segs = BarModel.segments(frozen, { show: ["cpu"] }, {}, "c", undefined, NOW)
  assert.deepEqual(segs.map(s => s.key), ["icon", "frozen", "cpu"])
  assert.deepEqual(BarModel.segments(minimal, { show: ["cpu"] }, {}, "c", undefined, NOW).map(s => s.key), ["icon", "cpu"])
})
