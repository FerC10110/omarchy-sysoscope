pragma Singleton
import QtQuick

// Stand-in for the shell's Color singleton: the theme comes from the JSON
// named by SHOTS_THEME (tools/shots/themes).
QtObject {
  readonly property var theme: {
    var x = new XMLHttpRequest()
    x.open("GET", "file://" + Qt.application.arguments[Qt.application.arguments.length - 1], false)
    x.send()
    return JSON.parse(x.responseText)
  }
  property color foreground: theme.foreground
  property color background: theme.background
  property color accent: theme.accent
  property color urgent: theme.urgent
  property color muted: theme.muted
}
