import QtQuick
import QtQuick.Layouts
import qs.Commons

// The core map: one square per logical CPU, filled from the bottom by its usage.
ColumnLayout {
  id: cores
  property var block: ({})
  property var host: null
  spacing: Style.space(4)

  Label { visible: text !== ""; host: cores.host; text: cores.block.title || "" }

  Flow {
    Layout.fillWidth: true
    spacing: Style.space(3)

    Repeater {
      model: cores.block.cells || []
      Rectangle {
        required property var modelData
        width: Style.space(30)
        height: Style.space(30)
        radius: Style.space(4)
        clip: true
        color: cores.host ? cores.host.card : "transparent"
        border.width: 1
        border.color: cores.host ? cores.host.track : "transparent"
        Rectangle {
          anchors.bottom: parent.bottom
          width: parent.width
          height: parent.height * Math.min(1, Math.max(0, parent.modelData.value))
          color: cores.host ? cores.host.strongColor(parent.modelData.level) : Color.accent
          opacity: 0.85
          Behavior on height { NumberAnimation { duration: 250; easing.type: Easing.OutCubic } }
        }
        Text {
          anchors.centerIn: parent
          text: parent.modelData.label
          textFormat: Text.PlainText
          // Over the fill (more than half the tile) the background colour reads.
          color: !cores.host ? Color.foreground : (parent.modelData.value > 0.5 ? cores.host.background : cores.host.foreground)
          font.family: cores.host ? cores.host.fontFamily : Style.font.family
          font.pixelSize: Style.font.caption
        }
      }
    }
  }
}
