import QtQuick
import QtQuick.Layouts
import qs.Commons

// A titled group of blocks (a disk, an interface, a GPU), indented.
ColumnLayout {
  id: group
  property var block: ({})
  property var host: null
  spacing: Style.space(6)

  Label {
    Layout.fillWidth: true
    Layout.topMargin: Style.space(6)
    visible: text !== ""
    host: group.host
    text: group.block.title || ""
  }

  Loader {
    Layout.fillWidth: true
    Layout.leftMargin: Style.space(12)
    source: "Blocks.qml"
    onLoaded: {
      item.host = Qt.binding(function() { return group.host })
      item.blocks = Qt.binding(function() { return group.block.blocks || [] })
    }
  }
}
