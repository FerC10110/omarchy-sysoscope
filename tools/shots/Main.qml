import QtQuick
import QtQuick.Window

Window {
  id: win
  readonly property var args: Qt.application.arguments.slice(Qt.application.arguments.indexOf("--") + 1)
  readonly property string section: args[1]
  width: section === "preview" ? 1920 : 900
  height: section === "preview" ? 1080 : section === "bar" ? (args[3] === "both" ? 80 : 40) : 670
  visible: true
  color: "black"

  FakeService { id: svc; dir: win.args[0] }

  Loader {
    id: loader
    source: win.section === "bar" ? "BarScene.qml" : win.section === "preview" ? "PreviewScene.qml" : "PanelScene.qml"
    onLoaded: {
      item.service = svc
      item.section = win.section
      item.look = win.args[3]
    }
  }

  Timer {
    interval: 800
    running: true
    onTriggered: win.contentItem.grabToImage(function(r) { r.saveToFile(win.args[2]); Qt.quit() })
  }
}
