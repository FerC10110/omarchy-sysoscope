const test = require("node:test")
const assert = require("node:assert")
const fs = require("node:fs")
const path = require("node:path")

const qml = fs.readFileSync(path.join(__dirname, "..", "Service.qml"), "utf8")

// The 60 s timer's first run usually comes before the bar has said which
// metrics it shows; the series are fetched again as soon as it does.
test("the long histories are fetched when the bar's keys arrive", () => {
  assert.match(qml, /onBarKeysChanged:\s*fetchSeries\(\)/)
})

// An unknown section would leave the panel on Loading… with no title.
test("IPC open refuses an unknown section", () => {
  assert.match(qml, /PanelModel\.knownSection\(service\.sections, section\)/)
})

// Launcher entries: installed on load, removed when the service goes away,
// unless the plugin still answers (a reload makes a new service at once).
test("the launcher entries are installed on load and removed on unload", () => {
  assert.match(qml, /import "Launcher\.js" as Launcher/)
  assert.match(qml, /Component\.onCompleted:\s*\{[^}]*installLauncher\(\)/)
  assert.match(qml, /Component\.onDestruction:\s*\{[^}]*Quickshell\.execDetached\(Launcher\.removeArgv\(/)
  assert.match(qml, /Launcher\.aliveCommand\(pluginId\)/)
})

test("IPC answers ping, which the removal checks", () => {
  assert.match(qml, /function ping\(\): string \{ return "ok" \}/)
})

test("a failed install goes to the shell's log", () => {
  assert.match(qml, /console\.warn\("sysoscope: launcher entries: "/)
})

// A full shell restart destroys the old service and creates a new one right
// away; the old one's removal (above) waits and pings, but a shell that is
// still starting may not answer yet, so it can delete the entries right
// after this service's own install already ran as a no-op. Reinstalling once
// more, well after the removal window, puts them back.
test("the entries are reinstalled once more after the removal window (shell restart)", () => {
  const removeWaitS = Number(qml.match(/Launcher\.removeArgv\(applicationsDir, pluginId, (\d+),/)[1])
  const timerBlock = qml.match(/Timer\s*\{\s*id:\s*reinstallTimer[\s\S]*?\n  \}/)
  assert.ok(timerBlock, "reinstallTimer not found")
  const interval = Number(timerBlock[0].match(/interval:\s*(\d+)/)[1])
  assert.ok(interval > removeWaitS * 1000, `interval ${interval}ms should exceed the removal wait of ${removeWaitS}s`)
  assert.match(timerBlock[0], /repeat:\s*false/)
  assert.match(timerBlock[0], /onTriggered:\s*service\.installLauncher\(\)/)
  assert.match(qml, /Component\.onCompleted:\s*\{[^}]*reinstallTimer\.running = true/)
})

// A search reply repeats the last sample: it must not add a second point to
// the bar's recent history.
test("a repeated sample adds no point to the bar", () => {
  assert.match(qml, /if \(Stream\.isNewSample\(snapshot, r\.snapshot\)\) recent = BarModel\.push\(recent, r\.snapshot\)/)
})

test("the search is sent as a JSON string and its reply is kept", () => {
  assert.match(qml, /function searchContent\(query\) \{ send\(Stream\.searchCommand\(query\)\) \}/)
  assert.match(qml, /if \(r\.search\) searchResult = r\.search/)
  assert.match(qml, /function clearSearch\(\) \{ searchResult = null \}/)
})
