const test = require("node:test")
const assert = require("node:assert")
const fs = require("node:fs")
const path = require("node:path")

// A ColumnLayout takes fillWidth from its children, so the sidebar grows with
// the view unless its width is pinned: it must not jump when the view changes.
test("the panel's sidebar has a fixed width", () => {
  const qml = fs.readFileSync(path.join(__dirname, "..", "Panel.qml"), "utf8")
  const sidebar = qml.split("// ---- sidebar")[1].split("spacing:")[0]
  assert.match(sidebar, /Layout\.fillWidth: false/)
  assert.match(sidebar, /Layout\.minimumWidth: Style\.space\(200\)/)
  assert.match(sidebar, /Layout\.maximumWidth: Style\.space\(200\)/)
})

test("the section title is the plugin's own, not the host's PanelHero", () => {
  const qml = fs.readFileSync(path.join(__dirname, "..", "Panel.qml"), "utf8")
  assert.doesNotMatch(qml, /PanelHero \{/)
})

test("the bar reads the look setting and draws segments with BarSegment", () => {
  const qml = fs.readFileSync(path.join(__dirname, "..", "BarWidget.qml"), "utf8")
  assert.match(qml, /look: setting\("look"/)
  assert.match(qml, /BarSegment \{/)
})
