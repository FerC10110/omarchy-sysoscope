// Run with: node --test tests/
const test = require("node:test")
const assert = require("node:assert/strict")
const I18n = require("../I18n.js")
const { sources } = require("./sources.js")

const CALL = /\bt\(\s*"((?:[^"\\]|\\.)*)"/g
const LABEL = /\blabel:\s*"([^"]*)"/g

function used() {
  const found = new Map()
  for (const f of sources([".qml", ".js"])) {
    if (f.name === "I18n.js") continue
    let m
    while ((m = CALL.exec(f.text))) found.set(JSON.parse('"' + m[1] + '"'), f.name)
    // BarModel.js and Settings.js translate the labels of their tables: t(m.label).
    if (f.name === "BarModel.js" || f.name === "Settings.js")
      while ((m = LABEL.exec(f.text))) found.set(m[1], f.name)
  }
  return found
}

test("every text passed to t() has a Spanish translation", () => {
  const missing = [...used()].filter(([s]) => !Object.prototype.hasOwnProperty.call(I18n.STRINGS.es, s))
  assert.deepEqual(missing.map(([s, f]) => `${f}: ${s}`), [])
})

test("every Spanish translation is used by some t() call", () => {
  const u = used()
  assert.deepEqual(Object.keys(I18n.STRINGS.es).filter(k => !u.has(k)), [])
})

test("every translation keeps the %N placeholders of its key", () => {
  const marks = s => [...new Set(s.match(/%\d+/g) || [])].sort()
  for (const [k, v] of Object.entries(I18n.STRINGS.es))
    assert.deepEqual(marks(v), marks(k), `placeholders differ in "${k}"`)
})
