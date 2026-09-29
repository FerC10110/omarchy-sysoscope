const test = require("node:test")
const assert = require("node:assert/strict")
const Theme = require("./load.js").load("Theme.js")

test("columns keep the newest values", () => {
  assert.deepEqual(Theme.columns([1, 2, 3, 4], 2, 0).shown, [3, 4])
})

test("columns drop what is not a number", () => {
  assert.deepEqual(Theme.columns([1, null, "x", 2, NaN], 10, 0).shown, [1, 2])
})

test("the scale is the highest value, or the ceiling when higher", () => {
  assert.equal(Theme.columns([5, 20], 10, 0).top, 20)
  assert.equal(Theme.columns([5, 20], 10, 100).top, 100)
})

test("no values or all zero never scale by zero", () => {
  assert.deepEqual(Theme.columns([], 10, 0), { shown: [], top: 1 })
  assert.equal(Theme.columns([0, 0], 10, 0).top, 1)
  assert.deepEqual(Theme.columns(undefined, 10, 0).shown, [])
})
