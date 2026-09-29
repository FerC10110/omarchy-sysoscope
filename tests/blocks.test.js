// Run with: node --test tests/
const test = require("node:test")
const assert = require("node:assert/strict")
const fs = require("node:fs")
const path = require("node:path")

const root = path.join(__dirname, "..")
const blocksQml = fs.readFileSync(path.join(root, "blocks", "Blocks.qml"), "utf8")

test("the panel draws every block type the collector sends", () => {
  for (const t of ["kv", "meter", "spark", "table", "text", "group", "cores", "stat", "cards", "hero"])
    assert.match(blocksQml, new RegExp(`\\b${t}: "`), `Blocks.qml has no entry for ${t}`)
})

test("only headline numbers glow", () => {
  const withEffect = fs.readdirSync(path.join(root, "blocks")).filter(f =>
    /MultiEffect/.test(fs.readFileSync(path.join(root, "blocks", f), "utf8")))
  assert.deepEqual(withEffect, ["StatBlock.qml"])
})

const read = f => fs.readFileSync(path.join(root, "blocks", f), "utf8")

test("sparklines are columns, not a line", () => {
  assert.doesNotMatch(read("SparkBlock.qml"), /Canvas/)
  assert.match(read("SparkBlock.qml"), /Columns \{/)
})

test("titles of kv, table, group and cores are spaced capitals", () => {
  for (const f of ["KvBlock.qml", "TableBlock.qml", "GroupBlock.qml", "CoresBlock.qml"])
    assert.match(read(f), /Label \{/, `${f} does not use Label`)
})

test("warnings are drawn as a stripe with a coloured left edge", () => {
  assert.match(read("TextBlock.qml"), /stripe/)
})

test("the bar draws a tagless segment's icon, not the start of its text", () => {
  const qml = fs.readFileSync(path.join(root, "BarSegment.qml"), "utf8")
  assert.match(qml, /seg\.segment\.icon/)
  assert.doesNotMatch(qml, /text\.split\(" "\)\[0\]/)
})

test("a core tile names its core and stays readable over the fill", () => {
  const qml = read("CoresBlock.qml")
  assert.match(qml, /modelData\.label/)
  assert.match(qml, /host\.background/)
})

test("columns in a card are few enough to read as columns", () => {
  assert.match(read("StatBlock.qml"), /maxColumns: 24/)
})

test("in a thin bar the tag is one readable line, stacked only in a tall one", () => {
  const qml = fs.readFileSync(path.join(root, "BarSegment.qml"), "utf8")
  assert.match(qml, /readonly property bool stackTag: seg\.barHeight >= Style\.space\(36\)/)
  assert.match(qml, /id: flatTag/)
})

test("a text with no level is secondary, not body colour", () => {
  assert.match(read("TextBlock.qml"), /block\.level \|\| ""\) === "" \? line\.host\.dim/)
})

test("StatBlock has no size switch nobody sets", () => {
  assert.doesNotMatch(read("StatBlock.qml"), /\bbig\b/)
})

test("a bar segment with no series reserves no room for columns", () => {
  const seg = fs.readFileSync(path.join(root, "BarSegment.qml"), "utf8")
  assert.match(seg, /visible: seg\.segment\.style === "spark" && \(seg\.segment\.series \|\| \[\]\)\.length > 1/)
})
