import QtQuick
import QtQuick.Effects
import QtQuick.Layouts
import qs.Commons

// A headline number: label, number and unit, detail lines, and the recent
// values as columns or a fill bar when the collector sends them.
ColumnLayout {
  id: stat
  property var block: ({})
  property var host: null
  property bool showColumns: true
  // The widest the number may get before it elides. A row that sizes the stat
  // from its content (the hero) sets it, since `width` would depend on itself.
  property real numberWidth: width
  readonly property color numberColor: host ? host.strongColor(block.level) : Color.accent
  readonly property bool hasFill: typeof block.value === "number"
  spacing: Style.space(2)

  Label {
    Layout.fillWidth: true
    visible: text !== ""
    host: stat.host
    text: stat.block.label || ""
    color: stat.block.level === "warn" && stat.host ? stat.host.accent : (stat.host ? stat.host.dim : Color.muted)
  }

  RowLayout {
    Layout.fillWidth: true
    spacing: Style.space(4)
    Text {
      id: number
      Layout.maximumWidth: stat.numberWidth
      Layout.alignment: Qt.AlignBaseline
      text: stat.block.text || "—"
      textFormat: Text.PlainText
      elide: Text.ElideRight
      color: stat.numberColor
      font.family: stat.host ? stat.host.fontFamily : Style.font.family
      font.pixelSize: Math.round(Style.font.displayLarge * 1.6)
      font.bold: true
      // The software renderer draws no shader effects, and would hide the number.
      layer.enabled: GraphicsInfo.api !== GraphicsInfo.Software
      layer.effect: MultiEffect {
        shadowEnabled: true
        shadowColor: stat.numberColor
        shadowOpacity: 0.25
        shadowBlur: 0.6
        shadowHorizontalOffset: 0
        shadowVerticalOffset: 0
      }
    }
    Text {
      Layout.fillWidth: true
      Layout.alignment: Qt.AlignBaseline
      visible: text !== ""
      text: stat.block.unit || ""
      textFormat: Text.PlainText
      elide: Text.ElideRight
      color: stat.host ? stat.host.dim : Color.muted
      font.family: stat.host ? stat.host.fontFamily : Style.font.family
      font.pixelSize: Style.font.body
    }
  }

  Repeater {
    model: stat.block.lines || []
    Text {
      required property string modelData
      Layout.fillWidth: true
      text: modelData
      textFormat: Text.PlainText
      elide: Text.ElideRight
      color: stat.host ? stat.host.dim : Color.muted
      font.family: stat.host ? stat.host.fontFamily : Style.font.family
      font.pixelSize: Style.font.bodySmall
    }
  }

  Rectangle {
    Layout.fillWidth: true
    Layout.topMargin: Style.space(6)
    visible: stat.hasFill
    implicitHeight: Style.space(4)
    radius: height / 2
    color: stat.host ? stat.host.track : "transparent"
    Rectangle {
      width: parent.width * (stat.hasFill ? stat.block.value : 0)
      height: parent.height
      radius: parent.radius
      gradient: Gradient {
        orientation: Gradient.Horizontal
        GradientStop { position: 0; color: stat.numberColor }
        GradientStop { position: 1; color: stat.block.level === "crit" || !stat.host ? stat.numberColor : stat.host.accentHi }
      }
      Behavior on width { NumberAnimation { duration: 250; easing.type: Easing.OutCubic } }
    }
  }

  Columns {
    Layout.fillWidth: true
    Layout.topMargin: Style.space(6)
    visible: stat.showColumns && !stat.hasFill && (stat.block.values || []).length > 1
    implicitHeight: Style.space(28)
    maxColumns: 24
    values: stat.block.values || []
    ceiling: stat.block.unit === "%" ? 100 : 0
    color: stat.numberColor
    tipColor: stat.block.level === "crit" || !stat.host ? stat.numberColor : stat.host.accentHi
  }
}
