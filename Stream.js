// Pure helpers for Service.qml: which binary to run, how to read a line of
// `sysoscope stream`, and how long to wait before restarting it. No QML here,
// so node can test it.

var SCHEMA = 1

// `uname -m` → the suffix of bin/sysoscope-linux-<arch>, or "" when unsupported.
function archOf(uname) {
  var m = String(uname || "").trim()
  if (m === "x86_64" || m === "amd64") return "amd64"
  if (m === "aarch64" || m === "arm64") return "arm64"
  return ""
}

function binaryFor(arch) { return arch ? "bin/sysoscope-linux-" + arch : "" }

// One NDJSON line → { ok, snapshot, view, sections, result, search } or { ok: false, error }.
// error is "json" (not JSON) or "schema" (another schema: the plugin is old).
function parseLine(line) {
  var d
  try { d = JSON.parse(line) } catch (e) { return { ok: false, error: "json" } }
  if (!d || typeof d !== "object" || Array.isArray(d)) return { ok: false, error: "json" }
  if (d.schema !== SCHEMA) return { ok: false, error: "schema" }
  var view = d.view || null
  var sections = Array.isArray(d.sections) ? d.sections : []
  delete d.view
  delete d.sections
  var result = d.result || null
  delete d.result
  var search = d.search || null
  delete d.search
  return { ok: true, snapshot: d, view: view, sections: sections, result: result, search: search }
}

// The stdin command for an action. A row action sends its row and input as a
// JSON array: a row id can have spaces (app:comm:Web Content). A section
// action with an input (the Wi-Fi band) sends an empty row.
function actionCommand(id, row, input) {
  return row || input ? "action " + id + " " + JSON.stringify([row || "", input || ""]) : "action " + id
}

// The stdin command for a content search: the query as a JSON string, so
// spaces, quotes and accents reach the collector as typed.
function searchCommand(query) { return "search " + JSON.stringify(String(query || "")) }

// A line that repeats the last sample (a search reply, or a result while
// paused) is not a new point for the bar's recent history.
function isNewSample(prev, next) { return !prev || !next || prev.time !== next.time }

// 1 s, 2 s, 4 s … up to 60 s. attempt counts failed starts since the last
// run that lasted a minute.
// focusChange decides a panel's focus request. Each monitor has its own panel;
// opening one closes the other, and that close must not clear the new
// panel's focus. Returns the new {owner, id}, or null to ignore the request.
function focusChange(state, owner, id) {
  if (!id && state.owner && state.owner !== owner) return null
  return { owner: id ? owner : null, id: id || "" }
}

// The stdin commands the collector needs after the config changed (from
// Settings or from a hand edit of config.json), and whether the span did.
function configCommands(before, after) {
  var out = []
  if (after.interval !== before.interval) out.push("interval " + after.interval)
  if (after.units !== before.units) out.push("units " + after.units)
  if (after.cards.join(",") !== before.cards.join(",")) out.push("cards " + after.cards.join(","))
  return { commands: out, spanChanged: after.span !== before.span }
}

function backoffMs(attempt) {
  return Math.min(60000, 1000 * Math.pow(2, Math.max(0, attempt | 0)))
}

function streamArgs(cfg, lang) {
  return ["stream", "--interval", cfg.interval + "s", "--lang", lang,
          "--units", cfg.units, "--cards", cfg.cards.join(",")]
}

// `sysoscope history` for the bar's longer spans. The collector keeps 1-minute
// points for 24 h, so 1h is the last 60 points of 24h.
function historyArgs(metric, span) {
  return ["history", "--metric", metric, "--span", span === "7d" ? "7d" : "24h", "--format", "json"]
}

// The history JSON → its values, without the gaps (null) and, for 1h, only
// the last 60 points. Anything unreadable gives [].
function seriesFrom(text, span) {
  var d
  try { d = JSON.parse(text) } catch (e) { return [] }
  var pts = (d && Array.isArray(d.points)) ? d.points : []
  if (span === "1h") pts = pts.slice(-60)
  return pts.map(function(p) { return p && p.v }).filter(function(v) { return typeof v === "number" })
}

// The panel/tooltip text for a problem code. t is I18n.t bound to a language.
function problemText(code, detail, t) {
  switch (code) {
  case "arch": return t("Sysoscope has no collector for this processor (%1).", [detail])
  case "missing": return t("The collector is missing or not executable: %1", [detail])
  case "schema": return t("The collector and the plugin versions differ: update the plugin.")
  case "crashed": return t("The collector stopped; restarting in %1 s.", [detail])
  default: return ""
  }
}

if (typeof module !== "undefined") {
  module.exports = { SCHEMA: SCHEMA, focusChange: focusChange, configCommands: configCommands, archOf: archOf, binaryFor: binaryFor, parseLine: parseLine,
                     backoffMs: backoffMs, streamArgs: streamArgs, problemText: problemText,
                     historyArgs: historyArgs, seriesFrom: seriesFrom, actionCommand: actionCommand,
                     searchCommand: searchCommand, isNewSample: isNewSample }
}
