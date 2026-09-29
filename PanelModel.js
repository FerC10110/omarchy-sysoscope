// The panel's sidebar: its entries, the filter and what each key does. No
// QML here, so node can test it (tests/panelmodel.test.js).

var SETTINGS = { id: "settings", title: "Settings", group: "settings" }
var GROUPS = ["", "components", "system", "settings"]   // sidebar order

// The collector's sections plus Settings, in GROUPS order (stable inside a
// group). t translates.
function items(sections, t) {
  var list = (sections || []).slice()
  list.push({ id: SETTINGS.id, title: t("Settings"), group: SETTINGS.group })
  var rank = function(s) { var i = GROUPS.indexOf(s.group || ""); return i < 0 ? GROUPS.length : i }
  return list.map(function(s, i) { return { s: s, i: i } })
    .sort(function(a, b) { return rank(a.s) - rank(b.s) || a.i - b.i })
    .map(function(x) { return x.s })
}

// Lower case without accents: "Energía" → "energia".
function fold(s) {
  return String(s || "").normalize("NFD").replace(/[̀-ͯ]/g, "").toLowerCase()
}

function filtered(list, query) {
  var q = fold(query)
  return list.filter(function(s) { return fold(s.title).indexOf(q) >= 0 })
}

function indexOf(list, id) {
  for (var i = 0; i < list.length; i++) if (list[i].id === id) return i
  return -1
}

// state: { index, filtering, query }; key: a character from the key catcher,
// "Backspace" (or "\b") or "Escape". index is always a position in list (the
// full sidebar), also while filtering. Returns the new state plus action:
// "" | "refresh" | "close".
function onKey(state, key, list) {
  var s = { index: state.index, filtering: !!state.filtering, query: state.query || "", action: "" }
  if (s.filtering) {
    if (key === "Escape") { s.filtering = false; s.query = ""; return s }
    if (key === "Backspace" || key === "\b") s.query = s.query.slice(0, -1)
    else if (key.length === 1) s.query += key
    var first = filtered(list, s.query)[0]
    if (first) s.index = indexOf(list, first.id)
    return s
  }
  if (key === "Escape") { s.action = "close"; return s }
  if (key === "/") { s.filtering = true; s.query = ""; return s }
  if (key === "r") { s.action = "refresh"; return s }
  if (/^[1-9]$/.test(key)) {
    var n = Number(key) - 1
    if (n < list.length) s.index = n
    return s
  }
  if (key.length === 1) {
    var k = fold(key)
    for (var step = 1; step <= list.length; step++) {
      var j = (s.index + step) % list.length
      if (fold(list[j].title).charAt(0) === k) { s.index = j; break }
    }
  }
  return s
}

function groupTitle(group, t) {
  if (group === "components") return t("Components")
  if (group === "system") return t("System")
  return ""
}

// True for a section the panel can show: one of the collector's, or Settings.
function knownSection(sections, id) {
  if (!id) return false
  if (id === SETTINGS.id) return true
  return (sections || []).some(function(s) { return s.id === id })
}

// "Loading…" is shown while the section's view is on its way; not in
// Settings (drawn by the panel) nor when a problem means none is coming.
function loading(current, view, problem) {
  return current !== "settings" && !view && !problem
}

// One Nerd Font icon per section, drawn in the sidebar.
var SECTION_ICONS = {
  summary: "\u{F0570}", cpu: "\u{F061A}", memory: "\u{F035B}", gpu: "\u{F08AE}",
  storage: "\u{F02CA}", network: "\u{F0317}", sensors: "\u{F050F}", power: "\u{F140B}",
  about: "\u{F02FC}", maintenance: "\u{F05B7}", apps: "\u{F003B}", devices: "\u{F0A6A}", security: "\u{F0483}", settings: "\u{F0493}"
}

function iconOf(id) { return SECTION_ICONS[id] || "" }

// ---- rows and questions (the Applications section)

// The rows a view lets you pick, in order: [{ id, flags, label }], label
// being the first cell. Only a view with row actions has them; elsewhere j/k
// keep moving through the sections.
function rowsOf(view) {
  if (!view || !(view.actions || []).some(function(a) { return a.target === "row" })) return []
  var out = []
  function walk(blocks) {
    (blocks || []).forEach(function(b) {
      if (b.type === "table" && b.selectable)
        (b.rows || []).forEach(function(r) { out.push({ id: r.id, flags: r.flags || [], label: (r.cells || [])[0] || "" }) })
      walk(b.blocks)
    })
  }
  walk(view.blocks)
  return out
}

function rowIndex(rows, id) {
  for (var i = 0; i < rows.length; i++) if (rows[i].id === id) return i
  return -1
}

function moveRow(rows, id, dy) {
  if (rows.length === 0) return ""
  var i = rowIndex(rows, id)
  return rows[i < 0 ? 0 : Math.max(0, Math.min(rows.length - 1, i + dy))].id
}

// After a new sample: the same row if it is still there, else the one that
// took its place (or the last one), else the first.
function keepRow(before, after, id) {
  if (after.length === 0) return ""
  if (rowIndex(after, id) >= 0) return id
  var i = rowIndex(before, id)
  return after[i < 0 ? 0 : Math.min(after.length - 1, i)].id
}

// The actions the picked row can take: a row action is left out when the row
// has one of its hide flags, and when no row is picked.
function actionsFor(actions, row) {
  return (actions || []).filter(function(a) {
    if (a.target !== "row") return true
    return !!row && !(a.hide || []).some(function(f) { return row.flags.indexOf(f) >= 0 })
  })
}

function runOf(action, input) { return { run: { id: action.id, target: action.target, input: input || "" } } }

function afterConfirm(action) {
  if (action.input) return { ask: { action: action, stage: "input", value: action.input.default || "" } }
  return runOf(action, "")
}

// What starting an action does: ask to confirm, ask for its input, or run.
// label names the row a confirmation is about.
function askStart(action, label) {
  if (action.confirm) return { ask: { action: action, stage: "confirm", value: "", label: label || "" } }
  return afterConfirm(action)
}

function withValue(ask, value) { return { ask: { action: ask.action, stage: ask.stage, value: value, label: ask.label } } }

// One key while asking. A confirmation takes y/s (yes) or n; Enter is not a
// yes, so a stray Enter never closes an app. A number takes digits, a choice
// Left/Right; Return accepts, Escape cancels.
function askKey(ask, key) {
  var a = ask.action
  if (key === "Escape") return { cancel: true }
  if (ask.stage === "confirm") {
    if (key === "y" || key === "Y" || key === "s" || key === "S") return afterConfirm(a)
    if (key === "n" || key === "N") return { cancel: true }
    return { ask: ask }
  }
  var input = a.input, v = ask.value
  if (input.type === "choice") {
    var opts = input.options || [], i = opts.indexOf(v)
    if ((key === "Left" || key === "Right") && opts.length > 0) {
      i = i < 0 ? 0 : (i + (key === "Right" ? 1 : opts.length - 1)) % opts.length
      return withValue(ask, opts[i])
    }
    if (key === "Return") return i >= 0 ? runOf(a, v) : { ask: ask, error: "option" }
    return { ask: ask }
  }
  if (key === "Backspace" || key === "\b") return withValue(ask, v.slice(0, -1))
  if (key === "Return") {
    if (input.type === "number" && !/^\d+$/.test(v)) return { ask: ask, error: "number" }
    return runOf(a, v)
  }
  if (key.length === 1 && (input.type !== "number" || /^\d$/.test(key))) return withValue(ask, v + key)
  return { ask: ask }
}

// The footer while asking.
function askText(ask, error, t) {
  var a = ask.action, s
  if (ask.stage === "confirm") s = a.confirm + (ask.label ? " «" + ask.label + "»" : "") + " (" + t("y/n") + ")"
  else if (a.input.type === "choice") s = a.input.prompt + ": ‹ " + ask.value + " ›  " + t("←/→ enter")
  else s = a.input.prompt + ": " + ask.value + "▏"
  if (error === "number") s += " · " + t("a number")
  else if (error === "option") s += " · " + t("one of the options")
  return s
}

// ---- content search (the collector's `search`, under the filtered sections)

var SEARCH_MIN = 2

// True when the query is long enough to ask the collector.
function searchWanted(query) { return fold(query).trim().length >= SEARCH_MIN }

// The hits to show: the last reply, while the query still asks for one. A
// reply to a shorter query stays until the new one arrives (no flicker).
function hitsFor(reply, query) {
  if (!searchWanted(query) || !reply || !Array.isArray(reply.hits)) return []
  return reply.hits
}

// "Section › Path", the small line above a hit's text.
function hitPlace(hit) {
  return hit.path ? (hit.section_title || "") + " › " + hit.path : (hit.section_title || "")
}

// The filter's list: the sections that match, then the hits.
function searchList(sections, hits) {
  return (sections || []).map(function(s) { return { kind: "section", id: s.id } })
    .concat((hits || []).map(function(h, i) { return { kind: "hit", index: i } }))
}

// The cursor dy steps from cursor, kept inside the list.
function searchMove(list, cursor, dy) {
  if (list.length === 0) return 0
  return Math.max(0, Math.min(list.length - 1, (cursor < 0 ? 0 : cursor) + dy))
}

// What Enter (or a click) on the entry at cursor opens: a section, or a hit's
// section and its row (empty when the hit is not on a row you can pick).
function searchOpen(list, cursor, hits) {
  var e = list[cursor]
  if (!e) return null
  if (e.kind === "section") return { section: e.id, row: "", hit: false }
  var h = hits[e.index]
  return h ? { section: h.section, row: h.row || "", hit: true } : null
}

// After new rows arrive: the row a search asked for (pending) is picked as
// soon as the view has rows, and waits while it has none; otherwise keepRow.
function nextRow(before, after, selected, pending) {
  if (pending && after.length === 0) return { selected: "", pending: pending }
  if (pending && rowIndex(after, pending) >= 0) return { selected: pending, pending: "" }
  return { selected: keepRow(before, after, selected), pending: "" }
}

// A new reply can shrink or empty the hits while the cursor sits on one: it
// moves to the first hit, or to the last section when there is none, so it
// never points past the end of the list. On a section, untouched.
function afterHitsChange(sectionCount, cursor, hitCount) {
  if (cursor < sectionCount) return cursor
  if (hitCount > 0) return sectionCount
  return Math.max(0, sectionCount - 1)
}

// blocks/TableBlock.qml draws only its first ROW_LIMIT rows: a hit beyond
// that reach can still be selected (nextRow does not care), but it will
// never be drawn, so a scroll to it must not linger — a later refresh could
// otherwise bring it within reach and scroll there as a surprise, long after
// the search that asked for it.
var ROW_LIMIT = 50
function reachable(rows, id) {
  var i = rowIndex(rows, id)
  return i >= 0 && i < ROW_LIMIT
}

// ---- dragging the panel (bottom-right corner: size; title strip: place)

// The smallest panel that still fits the sidebar and a view, in Style.space
// units (scale is what one unit measures), and at most what fits on screen
// (0: no limit known yet).
function clampSize(w, h, maxW, maxH, scale) {
  var minW = Math.round(560 * scale), minH = Math.round(360 * scale)
  var hiW = maxW > 0 ? Math.max(minW, maxW) : 4000, hiH = maxH > 0 ? Math.max(minH, maxH) : 3000
  return { width: Math.round(Math.max(minW, Math.min(hiW, w))), height: Math.round(Math.max(minH, Math.min(hiH, h))) }
}

// The offset from where the panel opens on its own, within the screen, and
// never towards the bar: a panel under the bar could not be grabbed back.
function clampOffset(x, y, barPos, screenW, screenH) {
  var mx = screenW > 0 ? screenW : 4000, my = screenH > 0 ? screenH : 4000
  var nx = Math.round(Math.max(-mx, Math.min(mx, x))), ny = Math.round(Math.max(-my, Math.min(my, y)))
  if (barPos === "bottom") ny = Math.min(0, ny)
  else if (barPos === "left") nx = Math.max(0, nx)
  else if (barPos === "right") nx = Math.min(0, nx)
  else ny = Math.max(0, ny)
  return { x: nx, y: ny }
}

// How the host places it: the offset along the bar moves the anchor, the one
// away from the bar widens the gap between bar and panel.
function placement(x, y, barPos, baseGap) {
  var horizontal = barPos !== "left" && barPos !== "right"
  var away = barPos === "bottom" ? -y : barPos === "left" ? x : barPos === "right" ? -x : y
  return { anchorX: horizontal ? x : 0, anchorY: horizontal ? 0 : y, gap: baseGap + Math.max(0, away) }
}

if (typeof module !== "undefined") {
  module.exports = { clampSize: clampSize, clampOffset: clampOffset, placement: placement, SECTION_ICONS: SECTION_ICONS, iconOf: iconOf, SETTINGS: SETTINGS, GROUPS: GROUPS, items: items, filtered: filtered, indexOf: indexOf,
                     onKey: onKey, groupTitle: groupTitle, fold: fold, loading: loading, knownSection: knownSection,
                     rowsOf: rowsOf, rowIndex: rowIndex, moveRow: moveRow, keepRow: keepRow, actionsFor: actionsFor, askStart: askStart, askKey: askKey, askText: askText,
                     searchWanted: searchWanted, hitsFor: hitsFor, hitPlace: hitPlace, searchList: searchList, searchMove: searchMove, searchOpen: searchOpen, nextRow: nextRow,
                     afterHitsChange: afterHitsChange, ROW_LIMIT: ROW_LIMIT, reachable: reachable }
}
