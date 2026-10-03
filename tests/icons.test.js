// Run with: node --test tests/
const test = require("node:test")
const assert = require("node:assert/strict")
const Icons = require("../Icons.js")

test("a metric gets the same icon in English and in Spanish", () => {
  assert.equal(Icons.metric("Memory"), Icons.metric("Memoria"))
  assert.equal(Icons.metric("Disk /"), Icons.metric("Disco /"))
  assert.equal(Icons.metric("Network"), Icons.metric("Red"))
  assert.equal(Icons.metric("Battery"), Icons.metric("Batería"))
  assert.equal(Icons.metric("CPU temperature"), Icons.metric("CPU"))
  assert.equal(Icons.metric("Something else"), "•")
  assert.equal(Icons.metric(""), "")
})

test("titles, groups and tables match in both languages", () => {
  assert.equal(Icons.title("Security features"), Icons.title("Funciones de seguridad"))
  assert.equal(Icons.title("Date and time"), Icons.title("Fecha y hora"))
  assert.equal(Icons.group("Disks"), Icons.group("Discos"))
  assert.equal(Icons.table("Apps with windows"), Icons.table("Apps con ventana"))
  assert.equal(Icons.table("Processes"), Icons.table("Procesos"))
  assert.equal(Icons.title(undefined), "󰍛")
})

test("\"red\" only matches the whole word", () => {
  assert.equal(Icons.table("Shared"), "󰍛")
  assert.equal(Icons.table("Red"), Icons.table("Network"))
})

test("attention is found in both languages", () => {
  assert.equal(Icons.attention("Needs attention"), true)
  assert.equal(Icons.attention("Requiere atención"), true)
  assert.equal(Icons.attention("Disks"), false)
})

test("a hero line says what it is, whatever its place", () => {
  assert.equal(Icons.heroLine("up 3 h 12 min"), Icons.heroLine("encendido 3 h 12 min"))
  assert.equal(Icons.heroLine("AMD Ryzen 7 7840U w/ Radeon 780M Graphics"), "")
  assert.equal(Icons.heroLine("AMD Radeon 780M · 15.5 GiB"), "")
  assert.equal(Icons.heroLine("15.5 GiB"), "")
  assert.equal(Icons.heroLine(""), "•")
})
