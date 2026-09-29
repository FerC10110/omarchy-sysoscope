// Colours derived from the theme for the panel and the bar, and the scale of
// the history columns. No QML here, so node can test it (tests/theme.test.js).
// A colour is { r, g, b, a } in 0..1, the shape of a QML color; QML turns the
// result back into a colour with Qt.rgba.

var WHITE = { r: 1, g: 1, b: 1, a: 1 }

function mix(a, b, t) {
  return { r: a.r + (b.r - a.r) * t, g: a.g + (b.g - a.g) * t, b: a.b + (b.b - a.b) * t, a: 1 }
}

// Toward the text colour, not "lighter": the same rule works on light themes.
function card(bg, fg) { return mix(bg, fg, 0.04) }
function track(bg, fg) { return mix(bg, fg, 0.09) }
function dim(fg) { return { r: fg.r, g: fg.g, b: fg.b, a: 0.55 } }
function hi(accent) { return mix(accent, WHITE, 0.35) }

// The newest `max` numbers and the height they are scaled to: the highest one,
// or `ceiling` (100 for percentages) when that is higher. Never 0.
function columns(values, max, ceiling) {
  var shown = (values || []).filter(function(v) { return typeof v === "number" && isFinite(v) }).slice(-max)
  var top = Math.max(ceiling || 0, Math.max.apply(null, shown.concat([0])))
  return { shown: shown, top: top > 0 ? top : 1 }
}

if (typeof module !== "undefined")
  module.exports = { mix: mix, card: card, track: track, dim: dim, hi: hi, columns: columns }
