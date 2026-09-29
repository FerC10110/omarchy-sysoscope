// The bar's short texts. A missing value (null or undefined) is "—", never 0.
// No QML here, so node can test it (tests/format.test.js).

var BLOCKS = "▁▂▃▄▅▆▇█"
var DASH = "—"

function missing(v) { return v === null || v === undefined || typeof v !== "number" || !isFinite(v) }

function pct(v) { return missing(v) ? DASH : Math.round(v) + "%" }

// c in °C; units "f" shows °F.
function temp(c, units) {
  if (missing(c)) return DASH
  return Math.round(units === "f" ? c * 9 / 5 + 32 : c) + "°"
}

function scaled(v, base, units) {
  var i = -1
  while (v >= base && i < units.length - 1) { v /= base; i++ }
  return i < 0 ? String(Math.round(v)) : v.toFixed(1) + units[i]
}

// Bytes per second, SI, without "B/s": 2100000 → "2.1M".
function rate(bps) { return missing(bps) ? DASH : scaled(bps, 1000, ["k", "M", "G", "T"]) }

// Bytes, IEC: 13000000000 → "12.1G".
function bytes(b) { return missing(b) ? DASH : scaled(b, 1024, ["K", "M", "G", "T"]) }

function block(frac) {
  var f = Math.min(1, Math.max(0, frac))
  return BLOCKS.charAt(Math.round(f * (BLOCKS.length - 1)))
}

// One block per value (the last 8), scaled to [lo, hi]; hi null means the
// largest of the values.
function spark(values, lo, hi) {
  var vs = (values || []).filter(function(v) { return !missing(v) }).slice(-8)
  if (vs.length === 0) return ""
  if (hi === null || hi === undefined) hi = Math.max.apply(null, vs)
  var span = hi - lo
  return vs.map(function(v) { return block(span > 0 ? (v - lo) / span : 0) }).join("")
}

// MHz from 1000 up as GHz with two decimals, like the collector: 2320 → "2.32 GHz".
function mhz(v) { return missing(v) ? DASH : v >= 1000 ? (v / 1000).toFixed(2) + " GHz" : Math.round(v) + " MHz" }

// One block for a fraction 0..1.
function gauge(frac) { return missing(frac) ? " " : block(frac) }

if (typeof module !== "undefined") {
  module.exports = { pct: pct, temp: temp, rate: rate, bytes: bytes, spark: spark, gauge: gauge, mhz: mhz }
}
