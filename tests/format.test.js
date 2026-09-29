// Run with: node --test tests/
const test = require("node:test")
const assert = require("node:assert/strict")
const Format = require("../Format.js")

test("pct rounds and shows a dash for missing values", () => {
  assert.equal(Format.pct(23.4), "23%")
  assert.equal(Format.pct(null), "—")
  assert.equal(Format.pct(undefined), "—")
  assert.equal(Format.pct(0), "0%")
})

test("temp in °C or °F", () => {
  assert.equal(Format.temp(41.3, "c"), "41°")
  assert.equal(Format.temp(41.3, "f"), "106°")
  assert.equal(Format.temp(null, "c"), "—")
})

test("rate is SI without the unit", () => {
  assert.equal(Format.rate(2100000), "2.1M")
  assert.equal(Format.rate(950), "950")
  assert.equal(Format.rate(0), "0")
  assert.equal(Format.rate(12500), "12.5k")
  assert.equal(Format.rate(null), "—")
})

test("bytes is IEC with one decimal", () => {
  assert.equal(Format.bytes(13000000000), "12.1G")
  assert.equal(Format.bytes(512), "512")
  assert.equal(Format.bytes(null), "—")
})

test("spark draws one block per value, up to the last 8", () => {
  assert.equal(Format.spark([0, 50, 100], 0, 100), "▁▅█")
  assert.equal(Format.spark([], 0, 100), "")
  assert.equal(Format.spark(null, 0, 100), "")
  assert.equal(Format.spark([1, 2, 3, 4, 5, 6, 7, 8, 9, 10], 0, 10).length, 8)
  assert.equal(Format.spark([5, 10], 0, null), "▅█")   // hi null → the maximum
  assert.equal(Format.spark([0, 0], 0, null), "▁▁")    // all zero: no division by zero
  assert.equal(Format.spark([200, -5], 0, 100), "█▁")  // clamped
})

test("gauge is one block for a fraction", () => {
  assert.equal(Format.gauge(0), "▁")
  assert.equal(Format.gauge(1), "█")
  assert.equal(Format.gauge(null), " ")
})

test("mhz switches to GHz from 1000", () => {
  assert.equal(Format.mhz(2320), "2.32 GHz")
  assert.equal(Format.mhz(800), "800 MHz")
  assert.equal(Format.mhz(null), "—")
})
