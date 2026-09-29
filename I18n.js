// The user-visible text of the Sysoscope panel and bar widget, in English and
// Spanish. The key is the English string, so English costs nothing: a text
// with no translation comes back as it went in. `%1`, `%2`… take the values
// in `args`, and a translation can put them in a different order.
//
// The collector translates its own views (sysoscope view --lang); this table
// only covers what the QML writes itself.
//
// No QML in here, so node can test it (tests/i18n.test.js); QML ignores the
// module.exports block at the end.

var STRINGS = {
  es: {
    // Stream.js
    "Sysoscope has no collector for this processor (%1).": "Sysoscope no tiene colector para este procesador (%1).",
    "The collector is missing or not executable: %1": "Falta el colector o no es ejecutable: %1",
    "The collector and the plugin versions differ: update the plugin.": "El colector y el plugin tienen versiones distintas: actualizá el plugin.",
    "The collector stopped; restarting in %1 s.": "El colector se detuvo; se reinicia en %1 s.",

    // BarModel.js (the labels are the METRICS table)
    "%1: %2 · peak %3 · %4": "%1: %2 · pico %3 · %4",
    "CPU": "CPU",
    "CPU temperature": "Temperatura de la CPU",
    "Memory": "Memoria",
    "GPU": "GPU",
    "GPU temperature": "Temperatura de la GPU",
    "VRAM": "VRAM",
    "Disk": "Disco",
    "Disk activity": "Actividad de disco",
    "Network": "Red",
    "Load": "Carga",
    "Battery": "Batería",
    "Power profile": "Perfil de energía",
    "Frozen: %1": "Congeladas: %1",
    "%1 (%2 min)": "%1 (%2 min)",

    // PanelModel.js, Panel.qml, blocks/
    "Settings": "Ajustes",
    "Components": "Componentes",
    "System": "Sistema",
    "Loading…": "Cargando…",
    "… %1 more": "… %1 más",
    "In the content": "En el contenido",
    "y/n": "s/n",
    "←/→ enter": "←/→ enter",
    "a number": "un número",
    "one of the options": "una de las opciones",

    // Settings.js, SettingsView.qml
    "icon": "ícono",
    "sparkline": "gráfico",
    "gauge": "indicador",
    "value": "valor",
    "threshold %1": "umbral %1",
    "only above threshold": "solo por encima del umbral",
    "Bar": "Barra",
    "Bar look": "Aspecto de la barra",
    "Minimal": "Minimalista",
    "Detailed": "Detallada",
    "%1 of %2": "%1 de %2",
    "shown": "visible",
    "hidden": "oculta",
    "Show the Sysoscope icon": "Mostrar el ícono de Sysoscope",
    "on": "sí",
    "off": "no",
    "Summary cards": "Tarjetas del Resumen",
    "General": "General",
    "Temperature": "Temperatura",
    "Refresh every": "Actualizar cada",
    "%1 s": "%1 s",
    "Panel size": "Tamaño del panel",
    "automatic": "automático",
    "drag its corner or its title; Space resets": "arrastrá la esquina o el título; Espacio lo restablece",
    "Chart period": "Período de los gráficos",
    "Language": "Idioma",
    "shared by every plugin": "compartido por todos los plugins",
    "Collector cost": "Costo del colector",
    "%1 ms per sample · %2 memory · every %3 s": "%1 ms por muestra · %2 de memoria · cada %3 s",
    "Disk /": "Disco /",
    "Power": "Energía",
    "j/k choose · Space show or change · h/l less/more · J/K move · s style · o only above threshold": "j/k elegir · Espacio mostrar o cambiar · h/l menos/más · J/K mover · s estilo · o solo por encima del umbral"
  }
}

// text: the English string. lang: "en" or "es"; anything else is English.
// args: the values for %1, %2, … in order.
function t(text, lang, args) {
  var table = STRINGS[lang]
  // hasOwnProperty, not `table[text] || text`: a text like "constructor" would
  // otherwise find Object.prototype, and an empty translation would look like
  // a missing one.
  var out = (table && Object.prototype.hasOwnProperty.call(table, text)) ? table[text] : text
  // Single pass: a value that contains %2 is not substituted again, and %10
  // is not mangled while filling %1. %0 and out-of-range markers stay as-is.
  return out.replace(/%(\d+)/g, function(match, digits) {
    var n = Number(digits)
    if (n === 0 || !args || n > args.length) return match
    return String(args[n - 1])
  })
}

if (typeof module !== "undefined") {
  module.exports = { STRINGS: STRINGS, t: t }
}
