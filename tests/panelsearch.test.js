// Run with: node --test tests/
// Panel.qml and TableBlock.qml wiring for the content search (the logic is
// in PanelModel.js and tested there).
const test = require("node:test")
const assert = require("node:assert")
const fs = require("node:fs")
const path = require("node:path")

const panel = fs.readFileSync(path.join(__dirname, "..", "Panel.qml"), "utf8")
const table = fs.readFileSync(path.join(__dirname, "..", "blocks", "TableBlock.qml"), "utf8")

test("the search is asked 150 ms after the last key, only with two letters", () => {
  assert.match(panel, /id: searchTimer\s+interval: 150/)
  assert.match(panel, /if \(PanelModel\.searchWanted\(text\)\) searchTimer\.restart\(\); else searchTimer\.stop\(\)/)
  assert.match(panel, /sysoscope\.service\.searchContent\(sysoscope\.keyState\.query\)/)
})

// A shell hot reload can keep an older Service.qml without the search.
test("an older service without the search does not break the panel", () => {
  assert.match(panel, /sysoscope\.service && sysoscope\.service\.searchContent/)
  assert.match(panel, /service && service\.clearSearch/)
})

test("the hits are listed under their heading and open on Enter or click", () => {
  assert.match(panel, /sysoscope\.t\("In the content"\)/)
  assert.match(panel, /Keys\.onReturnPressed: sysoscope\.openSearch\(sysoscope\.searchCursor\)/)
  assert.match(panel, /onClicked: sysoscope\.openSearch\(sysoscope\.shown\.length \+ hitRow\.index\)/)
})

test("the row of a hit is picked and scrolled to", () => {
  assert.match(panel, /PanelModel\.nextRow\(lastRows, rows, selectedRow, pendingRow\)/)
  assert.match(table, /table\.host\.reveal/)
})

// Review fix round 1 (Task 4): TableBlock's Component.onCompleted can fire
// before the GridLayout places the cell (y still 0), so reveal() must not
// give up — it waits, and TableBlock retries once the layout moves the cell.
test("an unplaced cell is not scrolled to, and TableBlock retries once it moves", () => {
  const reveal = panel.match(/function reveal\(item\) \{[\s\S]*?\n  \}/)
  assert.ok(reveal, "reveal(item) not found")
  assert.match(reveal[0], /if \(p\.y <= 0\) return/)
  // The guard must come before revealRow is cleared, so a retry stays possible.
  assert.ok(reveal[0].indexOf("if (p.y <= 0) return") < reveal[0].indexOf('revealRow = ""'))
  assert.match(table, /onYChanged: revealLater\(\)/)
})

// Review fix round 1 (Task 4): a reply that shrinks or empties the hits must
// not leave searchCursor pointing past the end of the list.
test("the cursor is kept in range when a new reply changes the hits", () => {
  assert.match(panel, /onHitsChanged: searchCursor = PanelModel\.afterHitsChange\(shown\.length, searchCursor, hits\.length\)/)
})

// Review fix round 1 (Task 4): clicking a section while filtering marks it,
// like landing on it with the arrows already did.
test("clicking a filtered section marks it too", () => {
  assert.match(panel, /sysoscope\.select\(modelData\.id\)\s*\n\s*if \(sysoscope\.keyState\.filtering\) sysoscope\.searchCursor = index/)
})

// Review fix round 1 (Task 4): a hit beyond TableBlock's 50-row cap is
// selected but never drawn; revealRow must not linger waiting for it.
test("revealRow is dropped for a hit's row TableBlock will never draw", () => {
  assert.match(panel, /if \(selectedRow === revealRow && !PanelModel\.reachable\(rows, revealRow\)\) revealRow = ""/)
  assert.match(panel, /if \(revealRow !== "" && selectedRow === revealRow && !PanelModel\.reachable\(rows, revealRow\)\) revealRow = ""/)
})

// Review fix round 2 (final): opening the filter with `/` must put the
// cursor on the current section, not always on shown[0] (the Summary) —
// otherwise Enter (or Down) with an empty query jumps away from it.
test("the filter opens with the cursor on the current section, not shown[0]", () => {
  const startFilter = panel.match(/function startFilter\(\) \{[\s\S]*?\n  \}/)
  assert.ok(startFilter, "startFilter() not found")
  assert.match(startFilter[0], /var i = PanelModel\.indexOf\(shown, current\)/)
  assert.match(startFilter[0], /searchCursor = Math\.max\(0, i\)/)
  assert.doesNotMatch(startFilter[0], /searchCursor = 0\b/)
})
