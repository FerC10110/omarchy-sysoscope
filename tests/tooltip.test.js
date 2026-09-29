const test = require("node:test")
const assert = require("node:assert")
const fs = require("node:fs")
const path = require("node:path")

// The host shows a bar tooltip only while target.tooltipHovered is true. A
// segment's own MouseArea takes the hover from the WidgetButton, so each
// segment must be the tooltip's target and say when it is hovered.
test("each bar segment is its own tooltip target", () => {
  const qml = fs.readFileSync(path.join(__dirname, "..", "BarWidget.qml"), "utf8")
  const calls = qml.match(/(show|hide)Tooltip\(([^,)]+)/g) || []
  assert.ok(calls.length >= 4, "segments show and hide tooltips")
  for (const c of calls) assert.doesNotMatch(c, /\(button$/, c)
  const hovered = qml.match(/readonly property bool tooltipHovered: segMouse\w*\.containsMouse/g) || []
  assert.equal(hovered.length, 2, "horizontal and vertical segments")
})
