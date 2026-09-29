// Run with: node --test tests/
// The install and remove scripts run for real, with sh, against a temporary
// directory: never ~/.local/share/applications. The "is the plugin alive"
// check is `true` or `false`, never omarchy-shell.
const test = require("node:test")
const assert = require("node:assert/strict")
const fs = require("node:fs")
const os = require("node:os")
const path = require("node:path")
const { spawnSync } = require("node:child_process")
const Launcher = require("../Launcher.js")

const ID = "io.github.ferc10110.sysoscope"
const PANEL = ID + ".desktop"
const TERMINAL = ID + "-terminal.desktop"

function tmpDir() { return fs.mkdtempSync(path.join(os.tmpdir(), "sysoscope-launcher-")) }
function run(argv) {
  const r = spawnSync(argv[0], argv.slice(1), { encoding: "utf8" })
  if (r.error) throw r.error
  return r
}
function install(dir) { return run(Launcher.installArgv(dir, ID)) }
function remove(dir, alive) { return run(Launcher.removeArgv(dir, ID, 0, alive)) }
function read(file) { return fs.readFileSync(file, "utf8") }

test("the two entries: the panel and the lazylazy tab", () => {
  const [panel, terminal] = Launcher.entries(ID)
  assert.equal(panel.file, PANEL)
  assert.equal(terminal.file, TERMINAL)
  for (const e of [panel, terminal]) {
    const lines = e.text.split("\n")
    assert.equal(lines[0], "[Desktop Entry]")
    assert.ok(lines.includes(Launcher.MARKER), e.file)
    assert.ok(lines.includes("Type=Application"), e.file)
    assert.ok(lines.includes("Categories=System;Monitor;"), e.file)
    assert.ok(lines.some(l => l.startsWith("Comment[es]=")), e.file)
    assert.ok(lines.some(l => l.startsWith("Name[es]=")), e.file)
    assert.ok(e.text.endsWith("\n"), e.file)
  }
  assert.ok(panel.text.includes("\nName=Sysoscope\n"))
  assert.ok(panel.text.includes("\nExec=omarchy-shell " + ID + " toggle\n"))
  assert.ok(panel.text.includes("\nTryExec=omarchy-shell\n"))
  assert.ok(terminal.text.includes("\nName=Sysoscope (terminal)\n"))
  assert.ok(terminal.text.includes("\nExec=omarchy-launch-tui lazylazy sysoscope\n"))
  assert.ok(terminal.text.includes("\nTryExec=lazylazy\n"))
})

test("applicationsDir follows XDG_DATA_HOME only when it is absolute", () => {
  assert.equal(Launcher.applicationsDir("/data", "/home/ana"), "/data/applications")
  assert.equal(Launcher.applicationsDir("", "/home/ana"), "/home/ana/.local/share/applications")
  assert.equal(Launcher.applicationsDir("rel", "/home/ana"), "/home/ana/.local/share/applications")
  assert.equal(Launcher.applicationsDir(undefined, "/home/ana"), "/home/ana/.local/share/applications")
})

test("the scripts only touch the paths they are given", () => {
  for (const s of [Launcher.INSTALL, Launcher.REMOVE]) assert.doesNotMatch(s, /HOME|\.local/)
  assert.equal(Launcher.aliveCommand(ID), "omarchy-shell " + ID + " ping")
})

test("install writes both entries, creating the directory", () => {
  const dir = path.join(tmpDir(), "applications")
  const r = install(dir)
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.stderr, "")
  const [panel, terminal] = Launcher.entries(ID)
  assert.equal(read(path.join(dir, PANEL)), panel.text)
  assert.equal(read(path.join(dir, TERMINAL)), terminal.text)
  assert.equal(fs.statSync(path.join(dir, PANEL)).mode & 0o777, 0o644)
  assert.deepEqual(fs.readdirSync(dir).sort(), [TERMINAL, PANEL].sort())
})

test("install is idempotent: an unchanged entry is not rewritten", () => {
  const dir = tmpDir()
  install(dir)
  const file = path.join(dir, PANEL)
  const past = new Date(2020, 0, 1)
  fs.utimesSync(file, past, past)
  const ino = fs.statSync(file).ino
  assert.equal(install(dir).status, 0)
  assert.equal(fs.statSync(file).mtime.getTime(), past.getTime())
  assert.equal(fs.statSync(file).ino, ino)
  assert.deepEqual(fs.readdirSync(dir).sort(), [TERMINAL, PANEL].sort())
})

test("install updates an entry of ours that changed", () => {
  const dir = tmpDir()
  fs.writeFileSync(path.join(dir, PANEL), "[Desktop Entry]\nName=Old\n" + Launcher.MARKER + "\n")
  assert.equal(install(dir).status, 0)
  assert.equal(read(path.join(dir, PANEL)), Launcher.entries(ID)[0].text)
})

test("a file without the marker is left alone", () => {
  const dir = tmpDir()
  const mine = "[Desktop Entry]\nName=Mine\nX-Sysoscope-Managed=true-ish\n"
  fs.writeFileSync(path.join(dir, TERMINAL), mine)
  assert.equal(install(dir).status, 0)
  assert.equal(read(path.join(dir, TERMINAL)), mine)
  assert.equal(remove(dir, "false").status, 0)
  assert.equal(read(path.join(dir, TERMINAL)), mine)
  assert.ok(!fs.existsSync(path.join(dir, PANEL)))
})

test("a symlink is left alone", () => {
  const base = tmpDir()
  const dir = path.join(base, "applications")
  fs.mkdirSync(dir)
  const target = path.join(base, "target.desktop")
  fs.writeFileSync(target, "[Desktop Entry]\nName=Target\n" + Launcher.MARKER + "\n")
  fs.symlinkSync(target, path.join(dir, PANEL))
  assert.equal(install(dir).status, 0)
  assert.ok(fs.lstatSync(path.join(dir, PANEL)).isSymbolicLink())
  assert.equal(read(target), "[Desktop Entry]\nName=Target\n" + Launcher.MARKER + "\n")
  assert.equal(remove(dir, "false").status, 0)
  assert.ok(fs.lstatSync(path.join(dir, PANEL)).isSymbolicLink())
  assert.ok(fs.existsSync(target))
  assert.ok(!fs.existsSync(path.join(dir, TERMINAL)))
})

test("remove deletes only our entries", () => {
  const dir = tmpDir()
  install(dir)
  fs.writeFileSync(path.join(dir, "other.desktop"), "[Desktop Entry]\nName=Other\n")
  assert.equal(remove(dir, "false").status, 0)
  assert.deepEqual(fs.readdirSync(dir), ["other.desktop"])
  assert.equal(remove(dir, "false").status, 0) // nothing left to delete: still fine
})

test("remove does nothing while the plugin still answers", () => {
  const dir = tmpDir()
  install(dir)
  assert.equal(remove(dir, "true").status, 0)
  assert.deepEqual(fs.readdirSync(dir).sort(), [TERMINAL, PANEL].sort())
})

test("a directory that cannot be created is reported, not fatal", () => {
  const base = tmpDir()
  fs.writeFileSync(path.join(base, "file"), "")
  const r = install(path.join(base, "file", "applications"))
  assert.equal(r.status, 1)
  assert.match(r.stderr, /cannot create/)
})
