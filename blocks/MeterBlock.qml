import QtQuick
import QtQuick.Layouts
import qs.Commons

// A labelled bar for a value 0..1, coloured by its level.
ColumnLayout {
  id: meter
  property var block: ({})
  property var host: null
  readonly property real value: typeof block.value === "number" ? block.value : 0
  spacing: Style.space(3)

  RowLayout {
    Layout.fillWidth: true
    Text {
      Layout.fillWidth: true
      text: meter.block.label || ""
      textFormat: Text.PlainText
      elide: Text.ElideRight
      color: meter.host ? meter.host.foreground : Color.foreground
      font.family: meter.host ? meter.host.fontFamily : Style.font.family
      font.pixelSize: Style.font.body
    }
    Text {
      text: meter.block.text || ""
      textFormat: Text.PlainText
      color: meter.host ? meter.host.strongColor(meter.block.level) : Color.foreground
      font.family: meter.host ? meter.host.fontFamily : Style.font.family
      font.pixelSize: Style.font.body
    }
  }

  Rectangle {
    Layout.fillWidth: true
    implicitHeight: Style.space(8)
    radius: Style.space(4)
    color: meter.host ? meter.host.track : "transparent"
    Rectangle {
      width: parent.width * meter.value
      height: parent.height
      radius: parent.radius
      gradient: Gradient {
        orientation: Gradient.Horizontal
        GradientStop { position: 0; color: meter.host ? meter.host.strongColor(meter.block.level) : Color.accent }
        GradientStop { position: 1; color: !meter.host ? Color.accent : (meter.block.level === "crit" ? meter.host.urgent : meter.host.accentHi) }
      }
      Behavior on width { NumberAnimation { duration: 250; easing.type: Easing.OutCubic } }
    }
  }
}
