import QtQuick
import QtQuick.Effects
import QtQuick.Layouts
import qs.Commons
import "../Icons.js" as Icons

// A headline number: label with metric icon, balanced number, detail lines
// with tree branches, and recent values as columns or a fill bar.
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
  spacing: Style.space(3)


  RowLayout {
    Layout.fillWidth: true
    spacing: Style.space(4)
    visible: (stat.block.label || "") !== ""

    Text {
      text: Icons.metric(stat.block.label)
      textFormat: Text.PlainText
      color: stat.block.level === "warn" && stat.host ? stat.host.accent : (stat.host ? stat.host.accent : Color.accent)
      font.family: stat.host ? stat.host.fontFamily : Style.font.family
      font.pixelSize: Style.font.caption
    }

    Label {
      Layout.fillWidth: true
      host: stat.host
      text: stat.block.label || ""
      color: stat.block.level === "warn" && stat.host ? stat.host.accent : (stat.host ? stat.host.dim : Color.muted)
    }
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
      font.pixelSize: Math.round(Style.font.displayLarge * 1.1)
      font.bold: true
      // The software renderer draws no shader effects, and would hide the number.
      layer.enabled: GraphicsInfo.api !== GraphicsInfo.Software
      layer.effect: MultiEffect {
        shadowEnabled: true
        shadowColor: stat.numberColor
        shadowOpacity: 0.2
        shadowBlur: 0.4
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
      font.pixelSize: Style.font.bodySmall
      font.bold: true
    }
  }

  Repeater {
    model: stat.block.lines || []
    RowLayout {
      required property string modelData
      required property int index
      Layout.fillWidth: true
      spacing: Style.space(4)

      Text {
        text: index === (stat.block.lines || []).length - 1 ? "└ " : "├ "
        textFormat: Text.PlainText
        color: stat.host ? stat.host.dim : Color.muted
        font.family: stat.host ? stat.host.fontFamily : Style.font.family
        font.pixelSize: Style.font.caption
        opacity: 0.5
      }

      Text {
        Layout.fillWidth: true
        text: modelData
        textFormat: Text.PlainText
        elide: Text.ElideRight
        color: stat.host ? stat.host.dim : Color.muted
        font.family: stat.host ? stat.host.fontFamily : Style.font.family
        font.pixelSize: Style.font.bodySmall
      }
    }
  }

  Rectangle {
    Layout.fillWidth: true
    Layout.topMargin: Style.space(4)
    visible: stat.hasFill
    implicitHeight: Style.space(5)
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

