// Run with: node --test tests/
const test = require("node:test")
const assert = require("node:assert/strict")
const Theme = require("./load.js").load("Theme.js")

const c = (r, g, b) => ({ r, g, b, a: 1 })
const near = (x, y) => assert.ok(Math.abs(x - y) < 1e-9, `${x} != ${y}`)

test("card mixes the background 4% toward the text", () => {
  near(Theme.card(c(0, 0, 0), c(1, 1, 1)).r, 0.04)
})

test("track mixes the background 9% toward the text", () => {
  near(Theme.track(c(0, 0, 0), c(1, 1, 1)).g, 0.09)
})

test("on a light theme cards and tracks get darker, not lighter", () => {
  const bg = c(1, 1, 1), fg = c(0.1, 0.1, 0.1)
  assert.ok(Theme.card(bg, fg).r < 1)
  assert.ok(Theme.track(bg, fg).r < Theme.card(bg, fg).r)
})

test("hi is the accent 35% toward white", () => {
  const h = Theme.hi(c(0.9, 0.5, 0.05))
  near(h.r, 0.9 + 0.1 * 0.35)
  near(h.b, 0.05 + 0.95 * 0.35)
})

test("dim keeps the text colour at 55% opacity", () => {
  assert.deepEqual(Theme.dim(c(0.7, 0.7, 0.7)), { r: 0.7, g: 0.7, b: 0.7, a: 0.55 })
})
