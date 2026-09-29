// Run with: node --test tests/
const test = require("node:test")
const assert = require("node:assert/strict")
const I18n = require("../I18n.js")

const KEY = "The collector stopped; restarting in %1 s."

test("English returns the text as it came in", () => {
  assert.equal(I18n.t("Summary", "en"), "Summary")
})

test("a text with no translation comes back in English", () => {
  assert.equal(I18n.t("Not translated yet", "es"), "Not translated yet")
})

test("Spanish returns the translation, with its arguments", () => {
  assert.equal(I18n.t(KEY, "es", [4]), "El colector se detuvo; se reinicia en 4 s.")
})

test("an unknown language falls back to English", () => {
  assert.equal(I18n.t("Summary", "de"), "Summary")
  assert.equal(I18n.t("Summary"), "Summary")
})

test("a translation can put the placeholders in another order", () => {
  I18n.STRINGS.es["%1 before %2"] = "%2 después de %1"
  try {
    assert.equal(I18n.t("%1 before %2", "es", ["a", "b"]), "b después de a")
  } finally {
    delete I18n.STRINGS.es["%1 before %2"]
  }
})

test("an argument containing %2 is not re-substituted", () => {
  assert.equal(I18n.t("%1 · %2", "en", ["Press %2", "3 × 10"]), "Press %2 · 3 × 10")
})

test("%10 resolves to the tenth argument, and is left alone with two", () => {
  assert.equal(I18n.t("%10", "en", ["a", "b", "c", "d", "e", "f", "g", "h", "i", "j"]), "j")
  assert.equal(I18n.t("%10", "en", ["a", "b"]), "%10")
})

test("%0, a bare % and an out-of-range marker are left untouched", () => {
  assert.equal(I18n.t("%0 and %", "en", ["a"]), "%0 and %")
  assert.equal(I18n.t("%1 and %3", "en", ["a", "b"]), "a and %3")
})

test("a key that exists on Object.prototype is not a translation", () => {
  assert.equal(I18n.t("constructor", "es"), "constructor")
  assert.equal(I18n.t("toString", "es", ["x"]), "toString")
})

test("an empty translation is a translation, not a missing one", () => {
  I18n.STRINGS.es["Nothing to say"] = ""
  try {
    assert.equal(I18n.t("Nothing to say", "es"), "")
  } finally {
    delete I18n.STRINGS.es["Nothing to say"]
  }
})
