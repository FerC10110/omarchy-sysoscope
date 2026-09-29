// Run with: node --test tests/
// Every Text in the plugin shows text that comes from the machine (device
// names, process names, SSIDs), so it must never be read as rich text.
const test = require("node:test")
const assert = require("node:assert/strict")
const { sources } = require("./sources.js")

// textBlocks returns, for each `Text {`, its line and the text up to the brace
// that closes it. Braces inside strings and comments are rare in QML and would
// only make a block longer, never hide a missing textFormat.
function textBlocks(src) {
  const out = []
  const re = /(^|[^\w.])Text\s*\{/g
  let m
  while ((m = re.exec(src))) {
    const open = src.indexOf("{", m.index + m[1].length)
    let depth = 0, end = open
    for (; end < src.length; end++) {
      if (src[end] === "{") depth++
      else if (src[end] === "}" && --depth === 0) break
    }
    out.push({ line: src.slice(0, open).split("\n").length, body: src.slice(open, end + 1) })
  }
  return out
}

// ownBody drops the nested blocks, so an inner Text's textFormat does not
// count for the one around it.
function ownBody(body) {
  let out = "", depth = 0
  for (const ch of body.slice(1, -1)) {
    if (ch === "{") depth++
    if (depth === 0) out += ch
    if (ch === "}") depth--
  }
  return out
}

// The lines of the Texts in src that do not set PlainText themselves.
function missingPlainText(src) {
  return textBlocks(src).filter(b => !/textFormat\s*:\s*Text\.PlainText/.test(ownBody(b.body))).map(b => b.line)
}

test("every Text sets textFormat: Text.PlainText", () => {
  const bad = []
  for (const f of sources([".qml"]))
    for (const line of missingPlainText(f.text)) bad.push(`${f.name}:${line}`)
  assert.deepEqual(bad, [])
})

test("an outer Text without PlainText is found even when an inner one has it", () => {
  const src = "Text {\n  text: a\n  Text { textFormat: Text.PlainText }\n}"
  assert.deepEqual(missingPlainText(src), [1])
  assert.deepEqual(missingPlainText("Text { textFormat: Text.PlainText\n Text { text: b } }"), [2])
})

test("the scan finds a Text without PlainText, and ignores other types", () => {
  const blocks = textBlocks("Item {\n  Text { text: a }\n  TextInput { }\n  Text{ textFormat: Text.PlainText }\n}")
  assert.equal(blocks.length, 2)
  assert.equal(blocks[0].line, 2)
  assert.doesNotMatch(blocks[0].body, /PlainText/)
})

module.exports = { textBlocks }
