// The glyphs the blocks put next to their titles and labels. The collector
// sends those already translated, so every word list has the English and the
// Spanish forms; the first entry whose word appears in the text wins.
//
// No QML in here, so node can test it (tests/icons.test.js); QML ignores the
// module.exports block at the end.

var DEFAULT = "󰍛"

var METRIC = [
  ["", ["cpu"]],
  ["", ["mem", "ram", "in use", "used", "en uso", "usada"]],
  ["󰋊", ["disk", "disco", "storage"]],
  ["󰈀", ["net", "red"]],
  ["", ["gpu"]],
  ["󰔏", ["temp", "sensor"]],
  ["󱐋", ["power", "energía", "batt", "batería"]],
  ["󱫐", ["uptime", "tiempo encendido"]],
  ["󰏖", ["package", "paquete"]],
]

var TITLE = [
  ["", ["system", "sistema", "hardware"]],
  ["󱫐", ["date", "time", "fecha", "hora"]],
  ["󰏖", ["package", "paquete"]],
  ["", ["boot", "arranque"]],
  ["󰒃", ["security", "seguridad"]],
  ["󰈀", ["network", "red"]],
  ["", ["cpu"]],
  ["", ["gpu"]],
  ["", ["memory", "memoria"]],
  ["󰋊", ["disk", "disco", "storage"]],
]

var GROUP = [
  ["", ["attention", "atención", "warning", "advertencia"]],
  ["󰋊", ["disk", "disco", "drive", "storage"]],
  ["", ["gpu"]],
  ["", ["cpu"]],
  ["󰈀", ["net", "red"]],
  ["󰔏", ["sensor"]],
  ["󱐋", ["power", "energía"]],
]

var TABLE = [
  ["", ["app", "window", "ventana"]],
  ["󰩪", ["device", "dispositivo", "audio"]],
  ["󰐊", ["process", "proceso"]],
  ["󰈀", ["network", "red", "wifi", "wi-fi"]],
  ["󰋊", ["disk", "disco", "mount", "montaje"]],
]

// "red" is also inside other words ("shared", "required"), so it only
// matches whole; the rest match anywhere ("net" in "Network").
function has(s, word) {
  if (word !== "red") return s.indexOf(word) >= 0
  return /(^|[^a-záéíóúñ])red($|[^a-záéíóúñ])/.test(s)
}

function pick(table, text, fallback) {
  if (!text) return fallback
  var s = String(text).toLowerCase()
  for (var i = 0; i < table.length; i++)
    for (var j = 0; j < table[i][1].length; j++)
      if (has(s, table[i][1][j])) return table[i][0]
  return fallback
}

function metric(label) { return label ? pick(METRIC, label, "•") : "" }
function title(t) { return pick(TITLE, t, DEFAULT) }
function group(t) { return pick(GROUP, t, DEFAULT) }
function table(t) { return pick(TABLE, t, DEFAULT) }
function attention(t) { return pick(GROUP, t, "") === "" }

// A line of the Summary's hero: the processor, then the GPUs and the memory
// joined by " · ", then "up 3 h" ("encendido 3 h"). Any of them may be
// missing, so the line says what it is, not its place.
function heroLine(str) {
  if (!str) return "•"
  var s = String(str).toLowerCase()
  if (/^(up|encendido) /.test(s)) return "󱫐"
  if (s.indexOf(" · ") >= 0) return ""
  if (/cpu|ghz|@|ryzen|core|xeon|epyc|processor|snapdragon|cortex/.test(s)) return ""
  if (/graphics|radeon|nvidia|geforce|gpu/.test(s)) return ""
  if (/\d\s*[kmgt]i?b$/.test(s)) return ""
  return "•"
}

if (typeof module !== "undefined") {
  module.exports = { metric: metric, title: title, group: group, table: table, attention: attention, heroLine: heroLine }
}
