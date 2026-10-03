import QtQuick
import QtQuick.Layouts
import qs.Commons
import "../Icons.js" as Icons

// A titled group of blocks styled as a cohesive Omarchy card.
Rectangle {
  id: group
  property var block: ({})
  property var host: null


  Layout.fillWidth: true
  implicitHeight: groupCol.implicitHeight + Style.space(20)
  radius: Style.space(8)
  color: group.host ? group.host.card : "transparent"
  border.width: 1
  border.color: Icons.attention(group.block.title) && group.host
          ? Qt.rgba(group.host.warn.r, group.host.warn.g, group.host.warn.b, 0.4)
          : (group.host ? group.host.track : "transparent")

  ColumnLayout {
    id: groupCol
    anchors.left: parent.left
    anchors.right: parent.right
    anchors.top: parent.top
    anchors.margins: Style.space(10)
    spacing: Style.space(8)

    RowLayout {
      Layout.fillWidth: true
      visible: (group.block.title || "") !== ""
      spacing: Style.space(6)

      Text {
        text: Icons.group(group.block.title)
        textFormat: Text.PlainText
        color: Icons.attention(group.block.title) && group.host
                ? group.host.warn
                : (group.host ? group.host.accent : Color.accent)
        font.family: group.host ? group.host.fontFamily : Style.font.family
        font.pixelSize: Style.font.caption
      }

      Label {
        Layout.fillWidth: true
        host: group.host
        text: group.block.title || ""
        color: Icons.attention(group.block.title) && group.host
                ? group.host.warn
                : (group.host ? group.host.dim : Color.muted)
      }
    }

    Rectangle {
      Layout.fillWidth: true
      height: 1
      visible: (group.block.title || "") !== ""
      color: group.host ? group.host.track : "transparent"
    }

    Loader {
      Layout.fillWidth: true
      source: "Blocks.qml"
      onLoaded: {
        item.host = Qt.binding(function() { return group.host })
        item.blocks = Qt.binding(function() { return group.block.blocks || [] })
      }
    }
  }
}

