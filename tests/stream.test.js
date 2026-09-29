// Run with: node --test tests/
const test = require("node:test")
const assert = require("node:assert/strict")
const Stream = require("../Stream.js")
const Settings = require("./load.js").load("Settings.js")
const I18n = require("../I18n.js")

test("archOf maps uname -m to a bundled binary", () => {
  assert.equal(Stream.archOf("x86_64\n"), "amd64")
  assert.equal(Stream.archOf("aarch64"), "arm64")
  assert.equal(Stream.archOf("riscv64"), "")
  assert.equal(Stream.binaryFor("amd64"), "bin/sysoscope-linux-amd64")
  assert.equal(Stream.binaryFor(""), "")
})

test("parseLine splits the snapshot, the view and the sections", () => {
  const line = JSON.stringify({ schema: 1, cpu: { model: "X" }, sections: [{ id: "cpu" }], view: { id: "cpu", blocks: [] } })
  const r = Stream.parseLine(line)
  assert.equal(r.ok, true)
  assert.deepEqual(r.sections, [{ id: "cpu" }])
  assert.equal(r.view.id, "cpu")
  assert.equal(r.snapshot.cpu.model, "X")
  assert.equal("view" in r.snapshot, false)
  assert.equal("sections" in r.snapshot, false)
})

test("parseLine without a view gives null and an empty sidebar", () => {
  const r = Stream.parseLine('{"schema":1}')
  assert.equal(r.view, null)
  assert.deepEqual(r.sections, [])
})

test("parseLine rejects other schemas and non-objects", () => {
  assert.deepEqual(Stream.parseLine('{"schema":2}'), { ok: false, error: "schema" })
  assert.deepEqual(Stream.parseLine("{"), { ok: false, error: "json" })
  assert.deepEqual(Stream.parseLine("[]"), { ok: false, error: "json" })
  assert.deepEqual(Stream.parseLine("null"), { ok: false, error: "json" })
})

test("backoffMs doubles up to a minute", () => {
  assert.deepEqual([0, 1, 2, 3, 4, 5, 6, 7].map(Stream.backoffMs),
                   [1000, 2000, 4000, 8000, 16000, 32000, 60000, 60000])
})

test("streamArgs passes the settings to the collector", () => {
  assert.deepEqual(Stream.streamArgs(Settings.normalizeConfig({}), "es"),
    ["stream", "--interval", "2s", "--lang", "es", "--units", "c", "--cards", "cpu,memory,gpu,storage,network"])
})

test("problemText is translated and carries its detail", () => {
  const t = (s, a) => I18n.t(s, "es", a)
  const text = Stream.problemText("crashed", "4", t)
  assert.match(text, /4/)
  assert.notEqual(text, Stream.problemText("crashed", "4", (s, a) => I18n.t(s, "en", a)))
  assert.equal(Stream.problemText("", "", t), "")
})

test("historyArgs asks for 24h when the bar wants 1h", () => {
  assert.deepEqual(Stream.historyArgs("cpu.usage", "1h"), ["history", "--metric", "cpu.usage", "--span", "24h", "--format", "json"])
  assert.deepEqual(Stream.historyArgs("net.rx", "7d"), ["history", "--metric", "net.rx", "--span", "7d", "--format", "json"])
})

test("seriesFrom keeps the non-null values, the last 60 for 1h", () => {
  const pts = Array.from({ length: 100 }, (_, i) => ({ t: "x", v: i % 10 === 0 ? null : i }))
  const text = JSON.stringify({ schema: 1, metric: "cpu.usage", points: pts })
  const day = Stream.seriesFrom(text, "24h")
  assert.equal(day.length, 90)
  assert.equal(day.indexOf(null), -1)
  assert.deepEqual(Stream.seriesFrom(text, "1h"), pts.slice(40).map(p => p.v).filter(v => v !== null))
  assert.deepEqual(Stream.seriesFrom("not json", "24h"), [])
  assert.deepEqual(Stream.seriesFrom("{}", "24h"), [])
})

test("focusChange: a panel closing does not clear another monitor's focus", () => {
  const A = {}, B = {}
  let s = { owner: null, id: "" }
  s = Stream.focusChange(s, A, "cpu")
  assert.deepEqual(s, { owner: A, id: "cpu" })
  s = Stream.focusChange(s, B, "gpu")          // B opens while A is open
  assert.deepEqual(s, { owner: B, id: "gpu" })
  assert.equal(Stream.focusChange(s, A, ""), null)   // A's close is ignored
  assert.deepEqual(Stream.focusChange(s, B, ""), { owner: null, id: "" })
})

test("configCommands: what the collector must hear after a config change", () => {
  const a = { interval: 2, units: "c", cards: ["cpu"], span: "2m" }
  assert.deepEqual(Stream.configCommands(a, a), { commands: [], spanChanged: false })
  assert.deepEqual(Stream.configCommands(a, { interval: 5, units: "f", cards: ["cpu", "gpu"], span: "1h" }),
                   { commands: ["interval 5", "units f", "cards cpu,gpu"], spanChanged: true })
})

test("actionCommand: the row and the input go as a JSON array", () => {
  assert.equal(Stream.actionCommand("firmware.check", "", ""), "action firmware.check")
  assert.equal(Stream.actionCommand("window.to_workspace", "app:comm:Web Content", "4"),
               'action window.to_workspace ["app:comm:Web Content","4"]')
  assert.equal(Stream.actionCommand("app.kill", "0xa1"), 'action app.kill ["0xa1",""]')
})

test("actionCommand: a section action with an input sends an empty row", () => {
  assert.equal(Stream.actionCommand("wifi.band", "", "5"), 'action wifi.band ["","5"]')
  assert.equal(Stream.actionCommand("wifi.rescan", "", ""), "action wifi.rescan")
})

test("parseLine takes the result of an action out of the snapshot", () => {
  const r = Stream.parseLine(JSON.stringify({ schema: 1, result: { id: "app.kill", ok: false, text: "no" } }))
  assert.deepEqual(r.result, { id: "app.kill", ok: false, text: "no" })
  assert.equal("result" in r.snapshot, false)
  assert.equal(Stream.parseLine('{"schema":1}').result, null)
})

test("parseLine keeps the search reply apart", () => {
  const reply = { query: "hdmi", hits: [{ section: "devices", section_title: "Devices", text: "HDMI" }] }
  const r = Stream.parseLine(JSON.stringify({ schema: 1, time: "t1", search: reply }))
  assert.deepEqual(r.search, reply)
  assert.equal("search" in r.snapshot, false)
  assert.equal(Stream.parseLine('{"schema":1}').search, null)
})

test("searchCommand sends the query as a JSON string", () => {
  assert.equal(Stream.searchCommand('wi-fi "casa"'), 'search "wi-fi \\"casa\\""')
  assert.equal(Stream.searchCommand("módulos"), 'search "módulos"')
  assert.equal(Stream.searchCommand(""), 'search ""')
  assert.equal(Stream.searchCommand(undefined), 'search ""')
})

test("isNewSample: a line that repeats the sample adds no point", () => {
  assert.equal(Stream.isNewSample(null, { time: "a" }), true)
  assert.equal(Stream.isNewSample({ time: "a" }, { time: "a" }), false)
  assert.equal(Stream.isNewSample({ time: "a" }, { time: "b" }), true)
})
