// Run with: node --test tests/
const test = require("node:test")
const assert = require("node:assert/strict")
const PanelModel = require("../PanelModel.js")

// As `sysoscope sections` prints them, shuffled.
const SECTIONS = [
  { id: "about", title: "About", group: "system", badge: 1 },
  { id: "summary", title: "Summary", group: "", badge: 3 },
  { id: "cpu", title: "CPU", group: "components" },
  { id: "memory", title: "Memory", group: "components" },
  { id: "gpu", title: "GPU", group: "components" },
  { id: "storage", title: "Disks", group: "components", badge: 2 },
  { id: "network", title: "Network", group: "components" },
  { id: "sensors", title: "Sensors", group: "components" },
  { id: "power", title: "Energía", group: "components" },
  { id: "devices", title: "Devices", group: "system" },
  { id: "security", title: "Security", group: "system" }
]
const id = s => s.id
const LIST = PanelModel.items(SECTIONS, s => s)
const start = (i) => ({ index: i || 0, filtering: false, query: "" })
const at = (st) => LIST[st.index].id

test("items keeps the groups in order and ends with Settings", () => {
  assert.deepEqual(LIST.map(id), ["summary", "cpu", "memory", "gpu", "storage", "network", "sensors", "power",
                                  "about", "devices", "security", "settings"])
  assert.equal(PanelModel.items(SECTIONS, s => "x" + s).slice(-1)[0].title, "xSettings")
})

test("filtered ignores case and accents", () => {
  assert.deepEqual(PanelModel.filtered(LIST, "energia").map(id), ["power"])
  assert.deepEqual(PanelModel.filtered(LIST, "S").map(id).slice(0, 2), ["summary", "storage"])
  assert.equal(PanelModel.filtered(LIST, "").length, LIST.length)
})

test("indexOf", () => {
  assert.equal(PanelModel.indexOf(LIST, "gpu"), 3)
  assert.equal(PanelModel.indexOf(LIST, "nope"), -1)
})

test("digits jump to a section", () => {
  assert.equal(PanelModel.onKey(start(), "3", LIST).index, 2)
  const five = LIST.slice(0, 5)
  assert.equal(PanelModel.onKey(start(1), "9", five).index, 1)
})

test("a letter cycles through the sections that start with it", () => {
  let s = PanelModel.onKey(start(), "d", LIST)
  assert.equal(at(s), "storage")
  s = PanelModel.onKey(s, "d", LIST)
  assert.equal(at(s), "devices")
  s = PanelModel.onKey(s, "D", LIST)
  assert.equal(at(s), "storage")
  assert.equal(at(PanelModel.onKey(start(), "e", LIST)), "power")
})

test("m alternates Memory and Maintenance", () => {
  const list = PanelModel.items(SECTIONS.concat([{ id: "maintenance", title: "Maintenance", group: "system" }]), s => s)
  const on = (st) => list[st.index].id
  let s = PanelModel.onKey(start(), "m", list)
  assert.equal(on(s), "memory")
  s = PanelModel.onKey(s, "m", list)
  assert.equal(on(s), "maintenance")
  s = PanelModel.onKey(s, "M", list)
  assert.equal(on(s), "memory")
})

test("r refreshes without moving", () => {
  const s = PanelModel.onKey(start(4), "r", LIST)
  assert.equal(s.action, "refresh")
  assert.equal(s.index, 4)
})

test("/ filters, letters build the query, backspace and Escape", () => {
  let s = PanelModel.onKey(start(), "/", LIST)
  assert.equal(s.filtering, true)
  s = PanelModel.onKey(s, "g", LIST)
  s = PanelModel.onKey(s, "p", LIST)
  assert.equal(s.query, "gp")
  assert.equal(at(s), "gpu")
  s = PanelModel.onKey(s, "Backspace", LIST)
  assert.equal(s.query, "g")
  s = PanelModel.onKey(s, "\b", LIST)
  assert.equal(s.query, "")
  s = PanelModel.onKey(s, "Escape", LIST)
  assert.deepEqual([s.filtering, s.query, s.action], [false, "", ""])
})

test("Escape without a filter closes", () => {
  assert.equal(PanelModel.onKey(start(), "Escape", LIST).action, "close")
})

test("groupTitle", () => {
  const t = s => "t:" + s
  assert.equal(PanelModel.groupTitle("", t), "")
  assert.equal(PanelModel.groupTitle("components", t), "t:Components")
  assert.equal(PanelModel.groupTitle("system", t), "t:System")
  assert.equal(PanelModel.groupTitle("settings", t), "")
})

test("loading shows only while a view is on its way", () => {
  assert.equal(PanelModel.loading("cpu", null, ""), true)
  assert.equal(PanelModel.loading("cpu", { id: "cpu" }, ""), false)
  assert.equal(PanelModel.loading("settings", null, ""), false)
  // With the collector missing or crashed nothing is coming: the error says so.
  assert.equal(PanelModel.loading("summary", null, "The collector is missing"), false)
})

test("knownSection: only the collector's sections and Settings", () => {
  const secs = [{ id: "summary" }, { id: "cpu" }]
  assert.equal(PanelModel.knownSection(secs, "cpu"), true)
  assert.equal(PanelModel.knownSection(secs, "settings"), true)
  assert.equal(PanelModel.knownSection(secs, "bogus"), false)
  assert.equal(PanelModel.knownSection(secs, ""), false)
})

test("every section and Settings has an icon", () => {
  for (const id of ["summary", "cpu", "memory", "gpu", "storage", "network", "sensors", "power", "about", "maintenance", "apps", "devices", "security", "settings"])
    assert.notEqual(PanelModel.iconOf(id), "", `no icon for ${id}`)
  assert.equal(PanelModel.iconOf("nope"), "")
  assert.equal(PanelModel.iconOf("maintenance"), "\u{F05B7}")
})

// ---- dragging the panel

test("a dragged size stays between the floor and what fits on the screen", () => {
  assert.deepEqual(PanelModel.clampSize(100, 100, 1800, 1000, 1), { width: 560, height: 360 })
  assert.deepEqual(PanelModel.clampSize(5000, 5000, 1800, 1000, 1), { width: 1800, height: 1000 })
  assert.deepEqual(PanelModel.clampSize(900.6, 700.2, 0, 0, 1), { width: 901, height: 700 })
  assert.deepEqual(PanelModel.clampSize(100, 100, 3000, 3000, 2), { width: 1120, height: 720 })
})

test("a dragged panel never goes under the bar", () => {
  assert.deepEqual(PanelModel.clampOffset(50, -80, "top", 3440, 1440), { x: 50, y: 0 })
  assert.deepEqual(PanelModel.clampOffset(50, 80, "bottom", 3440, 1440), { x: 50, y: 0 })
  assert.deepEqual(PanelModel.clampOffset(-20, 30, "left", 3440, 1440), { x: 0, y: 30 })
  assert.deepEqual(PanelModel.clampOffset(20, 30, "right", 3440, 1440), { x: 0, y: 30 })
  assert.deepEqual(PanelModel.clampOffset(99999, 99999, "top", 3440, 1440), { x: 3440, y: 1440 })
})

test("the offset along the bar moves the anchor; the one away from it widens the gap", () => {
  assert.deepEqual(PanelModel.placement(40, 25, "top", 8), { anchorX: 40, anchorY: 0, gap: 33 })
  assert.deepEqual(PanelModel.placement(40, -25, "bottom", 8), { anchorX: 40, anchorY: 0, gap: 33 })
  assert.deepEqual(PanelModel.placement(25, 40, "left", 8), { anchorX: 0, anchorY: 40, gap: 33 })
  assert.deepEqual(PanelModel.placement(-25, 40, "right", 8), { anchorX: 0, anchorY: 40, gap: 33 })
  assert.deepEqual(PanelModel.placement(0, 0, "top", 8), { anchorX: 0, anchorY: 0, gap: 8 })
})

// ---- rows and questions (the Applications section)

// A view like `sysoscope view apps`: two windows, one windowless protected app.
const APPS = {
  id: "apps",
  blocks: [
    { type: "hero" },
    { type: "group", title: "Chromium", blocks: [
      { type: "text", text: "CPU 12%" },
      { type: "table", selectable: true, columns: ["Window"], rows: [
        { id: "0xa1", cells: ["GitHub"] }, { id: "0xa2", cells: ["YouTube"] }] }] },
    { type: "table", selectable: true, title: "Without a window", columns: ["App"], rows: [
      { id: "app:comm:Hyprland", cells: ["Hyprland"], flags: ["protected", "nowindow"] }] },
    { type: "table", selectable: false, rows: [{ id: "ignored", cells: [] }] }
  ],
  actions: [
    { key: "enter", id: "window.focus", target: "row", hide: ["nowindow"] },
    { key: "w", id: "window.to_workspace", target: "row", hide: ["nowindow"], input: { type: "number", prompt: "Workspace" } },
    { key: "o", id: "window.to_monitor", target: "row", hide: ["nowindow"], input: { type: "choice", prompt: "Monitor", options: ["DP-1", "HDMI-A-1"], default: "DP-1" } },
    { key: "K", id: "app.kill", target: "row", hide: ["protected"], confirm: "Force-close the whole app?" },
    { key: "B", id: "app.btop", target: "row" }
  ]
}
const rows = PanelModel.rowsOf(APPS)
const keys = list => list.map(a => a.key).join("")

test("rowsOf: the selectable rows in order, with their flags", () => {
  assert.deepEqual(rows, [{ id: "0xa1", flags: [], label: "GitHub" }, { id: "0xa2", flags: [], label: "YouTube" },
                          { id: "app:comm:Hyprland", flags: ["protected", "nowindow"], label: "Hyprland" }])
  assert.deepEqual(PanelModel.rowsOf({ blocks: APPS.blocks, actions: [{ key: "f", target: "section" }] }), [])
  assert.deepEqual(PanelModel.rowsOf(null), [])
})

test("moveRow stays within the rows", () => {
  assert.equal(PanelModel.moveRow(rows, "0xa1", 1), "0xa2")
  assert.equal(PanelModel.moveRow(rows, "app:comm:Hyprland", 1), "app:comm:Hyprland")
  assert.equal(PanelModel.moveRow(rows, "0xa1", -1), "0xa1")
  assert.equal(PanelModel.moveRow(rows, "gone", 1), "0xa1")
  assert.equal(PanelModel.moveRow([], "", 1), "")
})

test("keepRow: the same row, else the one now in its place", () => {
  const after = rows.filter(r => r.id !== "0xa2")
  assert.equal(PanelModel.keepRow(rows, rows, "0xa2"), "0xa2")
  assert.equal(PanelModel.keepRow(rows, after, "0xa2"), "app:comm:Hyprland")
  assert.equal(PanelModel.keepRow(rows, rows.slice(0, 1), "app:comm:Hyprland"), "0xa1")
  assert.equal(PanelModel.keepRow([], rows, ""), "0xa1")
  assert.equal(PanelModel.keepRow(rows, [], "0xa1"), "")
})

test("actionsFor hides the actions a row cannot take", () => {
  assert.equal(keys(PanelModel.actionsFor(APPS.actions, rows[0])), "enterwoKB")
  assert.equal(keys(PanelModel.actionsFor(APPS.actions, rows[2])), "B")
  assert.equal(keys(PanelModel.actionsFor(APPS.actions, null)), "")
  assert.equal(keys(PanelModel.actionsFor([{ key: "f", target: "section" }], null)), "f")
})

test("an action without questions runs at once", () => {
  assert.deepEqual(PanelModel.askStart(APPS.actions[0]), { run: { id: "window.focus", target: "row", input: "" } })
})

test("a confirmation takes y or s, and n or Escape cancel", () => {
  const { ask } = PanelModel.askStart(APPS.actions[3])
  assert.deepEqual(PanelModel.askKey(ask, "s").run, { id: "app.kill", target: "row", input: "" })
  assert.deepEqual(PanelModel.askKey(ask, "y").run.id, "app.kill")
  assert.equal(PanelModel.askKey(ask, "n").cancel, true)
  assert.equal(PanelModel.askKey(ask, "Escape").cancel, true)
  assert.equal(PanelModel.askKey(ask, "Return").ask, ask)   // Enter is not a yes
})

test("a number takes digits and backspace, and Return checks it", () => {
  let r = PanelModel.askStart(APPS.actions[1])
  for (const k of ["4", "x", "2", "\b", "7"]) r = PanelModel.askKey(r.ask, k)
  assert.equal(r.ask.value, "47")
  assert.deepEqual(PanelModel.askKey(r.ask, "Return").run, { id: "window.to_workspace", target: "row", input: "47" })
  const empty = PanelModel.askStart(APPS.actions[1]).ask
  assert.equal(PanelModel.askKey(empty, "Return").error, "number")
})

test("a choice starts on its default and moves with Left and Right", () => {
  let r = PanelModel.askStart(APPS.actions[2])
  assert.equal(r.ask.value, "DP-1")
  r = PanelModel.askKey(r.ask, "Right")
  assert.equal(r.ask.value, "HDMI-A-1")
  r = PanelModel.askKey(r.ask, "Right")
  assert.equal(r.ask.value, "DP-1")
  r = PanelModel.askKey(r.ask, "Left")
  assert.deepEqual(PanelModel.askKey(r.ask, "Return").run.input, "HDMI-A-1")
})

test("askText: the question, the value and how to answer", () => {
  const t = (s, args) => s.replace(/%(\d+)/g, (m, n) => args[n - 1])
  assert.equal(PanelModel.askText(PanelModel.askStart(APPS.actions[3]).ask, "", t), "Force-close the whole app? (y/n)")
  let r = PanelModel.askKey(PanelModel.askStart(APPS.actions[1]).ask, "4")
  assert.equal(PanelModel.askText(r.ask, "", t), "Workspace: 4▏")
  assert.equal(PanelModel.askText(PanelModel.askStart(APPS.actions[1]).ask, "number", t), "Workspace: ▏ · a number")
  assert.equal(PanelModel.askText(PanelModel.askStart(APPS.actions[2]).ask, "", t), "Monitor: ‹ DP-1 ›  ←/→ enter")
})

test("askText names the row the confirmation is about (spec §4)", () => {
  const t = s => s
  const ask = PanelModel.askStart(APPS.actions[3], rows[0].label).ask
  assert.equal(PanelModel.askText(ask, "", t), "Force-close the whole app? «GitHub» (y/n)")
  // The name stays while typing an answer that is not one.
  assert.equal(PanelModel.askText(PanelModel.askKey(ask, "q").ask, "", t), "Force-close the whole app? «GitHub» (y/n)")
})

test("on Devices each row gets only its kind of actions, and its own v", () => {
  const vol = d => ({ type: "number", prompt: "Volume (0–100)", default: d })
  const actions = [
    { key: "d", id: "audio.default", target: "row", hide: ["bt", "default"] },
    { key: "m", id: "audio.mute", target: "row", hide: ["bt"] },
    { key: "+", id: "audio.up", target: "row", hide: ["bt"] },
    { key: "-", id: "audio.down", target: "row", hide: ["bt"] },
    { key: "v", id: "audio.volume", target: "row", hide: ["bt", "node:hdmi"], input: vol("80") },
    { key: "v", id: "audio.volume", target: "row", hide: ["bt", "node:analog"], input: vol("40") },
    { key: "b", id: "bt.power", target: "section" },
    { key: "e", id: "bt.pair", target: "section" },
    { key: "c", id: "bt.connect", target: "row", hide: ["audio", "connected", "nearby"] },
    { key: "x", id: "bt.disconnect", target: "row", hide: ["audio", "disconnected"] },
    { key: "f", id: "bt.forget", target: "row", hide: ["audio", "nearby"], confirm: "Forget this device?" }
  ]
  const view = { actions, blocks: [
    { type: "table", selectable: true, rows: [
      { id: "analog", flags: ["audio", "output", "node:analog", "default"], cells: ["ALC1220 Analog"] },
      { id: "hdmi", flags: ["audio", "output", "node:hdmi"], cells: ["S34J55x"] } ] },
    { type: "table", selectable: true, rows: [
      { id: "AA", flags: ["bt", "connected", "paired"], cells: ["Headphones"] },
      { id: "BB", flags: ["bt", "disconnected", "paired"], cells: ["Keyboard"] },
      { id: "CC", flags: ["bt", "disconnected", "nearby"], cells: ["Speaker"] } ] } ] }
  const rows = PanelModel.rowsOf(view)
  const at = id => PanelModel.actionsFor(actions, rows.find(r => r.id === id))
  assert.equal(rows.length, 5)
  assert.equal(keys(at("analog")), "m+-vbe")
  assert.equal(keys(at("hdmi")), "dm+-vbe")
  assert.equal(at("analog").find(a => a.key === "v").input.default, "80")
  assert.equal(at("hdmi").find(a => a.key === "v").input.default, "40")
  assert.equal(keys(at("AA")), "bexf")
  assert.equal(keys(at("BB")), "becf")
  assert.equal(keys(at("CC")), "be")
})

// ---- content search

const HITS = [
  { section: "devices", section_title: "Devices", path: "Outputs", text: "S34J55x · 40 % · No", row: "alsa_output.hdmi" },
  { section: "cpu", section_title: "CPU", path: "", text: "Model: Ryzen" }
]

test("searchWanted needs two letters, accents aside", () => {
  assert.equal(PanelModel.searchWanted("h"), false)
  assert.equal(PanelModel.searchWanted(" h "), false)
  assert.equal(PanelModel.searchWanted("hd"), true)
  assert.equal(PanelModel.searchWanted("é"), false)
})

test("hitsFor shows the last reply only while the query asks for one", () => {
  const reply = { query: "hdm", hits: HITS }
  assert.deepEqual(PanelModel.hitsFor(reply, "hdmi"), HITS)
  assert.deepEqual(PanelModel.hitsFor(reply, "h"), [])
  assert.deepEqual(PanelModel.hitsFor(null, "hdmi"), [])
  assert.deepEqual(PanelModel.hitsFor({ query: "x" }, "hdmi"), [])
})

test("hitPlace joins the section and the path", () => {
  assert.equal(PanelModel.hitPlace(HITS[0]), "Devices › Outputs")
  assert.equal(PanelModel.hitPlace(HITS[1]), "CPU")
})

test("the search list is the sections, then the hits", () => {
  const list = PanelModel.searchList([{ id: "devices" }, { id: "security" }], HITS)
  assert.deepEqual(list, [{ kind: "section", id: "devices" }, { kind: "section", id: "security" },
                          { kind: "hit", index: 0 }, { kind: "hit", index: 1 }])
  assert.equal(PanelModel.searchMove(list, 0, 1), 1)
  assert.equal(PanelModel.searchMove(list, 1, 1), 2)
  assert.equal(PanelModel.searchMove(list, 3, 1), 3)
  assert.equal(PanelModel.searchMove(list, 0, -1), 0)
  assert.equal(PanelModel.searchMove(list, -1, 1), 1)
  assert.equal(PanelModel.searchMove([], 2, 1), 0)
})

test("searchOpen opens a section, or a hit's section and row", () => {
  const list = PanelModel.searchList([{ id: "security" }], HITS)
  assert.deepEqual(PanelModel.searchOpen(list, 0, HITS), { section: "security", row: "", hit: false })
  assert.deepEqual(PanelModel.searchOpen(list, 1, HITS), { section: "devices", row: "alsa_output.hdmi", hit: true })
  assert.deepEqual(PanelModel.searchOpen(list, 2, HITS), { section: "cpu", row: "", hit: true })
  assert.equal(PanelModel.searchOpen(list, 3, HITS), null)
  assert.equal(PanelModel.searchOpen([], 0, []), null)
})

test("nextRow picks the row a search asked for as soon as it is there", () => {
  const rows = [{ id: "a" }, { id: "b" }]
  // The view is still on its way: the pending row waits.
  assert.deepEqual(PanelModel.nextRow([], [], "", "b"), { selected: "", pending: "b" })
  assert.deepEqual(PanelModel.nextRow([], rows, "", "b"), { selected: "b", pending: "" })
  // Gone meanwhile: the usual choice, and the wish is dropped.
  assert.deepEqual(PanelModel.nextRow([], rows, "", "z"), { selected: "a", pending: "" })
  // Nothing pending: keepRow.
  assert.deepEqual(PanelModel.nextRow(rows, rows, "b", ""), { selected: "b", pending: "" })
})

test("afterHitsChange keeps the cursor on a section, but never past a shrunk hit list", () => {
  // On a section: untouched, whatever the hits do.
  assert.equal(PanelModel.afterHitsChange(2, 0, 5), 0)
  assert.equal(PanelModel.afterHitsChange(2, 1, 0), 1)
  // On a hit: the first hit of the new reply…
  assert.equal(PanelModel.afterHitsChange(2, 4, 3), 2)
  // …or the last section, when the new reply has none.
  assert.equal(PanelModel.afterHitsChange(2, 4, 0), 1)
  // No sections either: 0.
  assert.equal(PanelModel.afterHitsChange(0, 3, 0), 0)
})

test("reachable: a hit's row only if TableBlock would draw it (its first 50)", () => {
  const rows = Array.from({ length: 60 }, (_, i) => ({ id: "r" + i }))
  assert.equal(PanelModel.reachable(rows, "r0"), true)
  assert.equal(PanelModel.reachable(rows, "r49"), true)
  assert.equal(PanelModel.reachable(rows, "r50"), false)
  assert.equal(PanelModel.reachable(rows, "gone"), false)
  assert.equal(PanelModel.reachable([], "r0"), false)
})
