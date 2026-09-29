import QtQuick
import qs.Commons
import "blocks"
import "Theme.js" as Theme

// One metric in the horizontal bar. The minimal look is the icon-and-value
// text; the detailed one is a small tag (stacked in a tall bar), then the value, columns or a
// vertical gauge, and the network as two stacked lines.
Item {
  id: seg
  // Not `data`: that is Item's list of children.
  property var segment: ({})
  property string look: "detailed"
  property string fontFamily: Style.font.family
  property int fontSize: Style.font.body
  property color foreground: Color.foreground
  property color urgentColor: Color.urgent
  property color accent: Color.accent
  property real barHeight: Style.space(26)
  readonly property bool detailed: look !== "minimal" && segment.key !== "icon" && segment.style !== "icon"
  // Three stacked letters only read in a tall bar (about 12 px each).
  readonly property bool stackTag: seg.barHeight >= Style.space(36)
  readonly property color valueColor: segment.urgent ? urgentColor : foreground
  readonly property color fill: segment.urgent ? urgentColor : accent
  implicitWidth: detailed ? row.implicitWidth : plain.implicitWidth
  implicitHeight: barHeight

  Text {
    id: plain
    anchors.verticalCenter: parent.verticalCenter
    visible: !seg.detailed
    text: seg.segment.text || ""
    textFormat: Text.PlainText
    color: seg.valueColor
    font.family: seg.fontFamily
    font.pixelSize: seg.fontSize
    renderType: Text.NativeRendering
  }

  Row {
    id: row
    anchors.verticalCenter: parent.verticalCenter
    visible: seg.detailed
    spacing: Style.space(5)

    // The tag, in a thin bar: one small dim line before the value.
    Text {
      id: flatTag
      anchors.verticalCenter: parent.verticalCenter
      visible: !!seg.segment.tag && !seg.stackTag
      text: seg.segment.tag || ""
      textFormat: Text.PlainText
      color: Qt.rgba(seg.foreground.r, seg.foreground.g, seg.foreground.b, 0.55)
      font.family: seg.fontFamily
      font.pixelSize: Math.max(9, Math.round(seg.fontSize * 0.75))
      font.letterSpacing: 0.5
    }

    // The tag, in a tall bar: three letters stacked.
    Column {
      anchors.verticalCenter: parent.verticalCenter
      visible: !!seg.segment.tag && seg.stackTag
      Repeater {
        model: (seg.segment.tag || "").split("")
        Text {
          required property string modelData
          text: modelData
          textFormat: Text.PlainText
          color: Qt.rgba(seg.foreground.r, seg.foreground.g, seg.foreground.b, 0.55)
          font.family: seg.fontFamily
          font.pixelSize: Math.max(6, Math.floor(seg.barHeight / 3.4))
          lineHeight: 0.8
          horizontalAlignment: Text.AlignHCenter
        }
      }
    }

    Text {
      anchors.verticalCenter: parent.verticalCenter
      visible: !seg.segment.tag
      text: seg.segment.icon || ""
      textFormat: Text.PlainText
      color: seg.valueColor
      font.family: seg.fontFamily
      font.pixelSize: seg.fontSize
    }

    Columns {
      anchors.verticalCenter: parent.verticalCenter
      visible: seg.segment.style === "spark" && (seg.segment.series || []).length > 1
      width: Style.space(46)
      height: seg.barHeight * 0.6
      maxColumns: 14
      values: seg.segment.series || []
      ceiling: seg.segment.frac !== null && seg.segment.frac !== undefined ? 100 : 0
      color: seg.fill
      tipColor: seg.segment.urgent ? seg.fill : Qt.rgba(Theme.hi(seg.accent).r, Theme.hi(seg.accent).g, Theme.hi(seg.accent).b, 1)
    }

    Rectangle {
      anchors.verticalCenter: parent.verticalCenter
      visible: seg.segment.style === "gauge" && seg.segment.frac !== null && seg.segment.frac !== undefined
      width: Style.space(8)
      height: seg.barHeight * 0.7
      radius: Style.space(2)
      color: Qt.rgba(seg.foreground.r, seg.foreground.g, seg.foreground.b, 0.15)
      Rectangle {
        anchors.bottom: parent.bottom
        width: parent.width
        height: parent.height * (seg.segment.frac || 0)
        radius: parent.radius
        color: seg.fill
        Behavior on height { NumberAnimation { duration: 250; easing.type: Easing.OutCubic } }
      }
    }

    // Network: download over upload.
    Column {
      anchors.verticalCenter: parent.verticalCenter
      visible: seg.segment.key === "net"
      Repeater {
        model: seg.segment.key === "net" ? ["↓ " + seg.segment.value, seg.segment.sub || ""] : []
        Text {
          required property string modelData
          text: modelData
          textFormat: Text.PlainText
          color: seg.valueColor
          font.family: seg.fontFamily
          font.pixelSize: Math.max(7, Math.floor(seg.fontSize * 0.75))
          lineHeight: 0.85
        }
      }
    }

    Text {
      anchors.verticalCenter: parent.verticalCenter
      visible: seg.segment.key !== "net"
      text: seg.segment.value || ""
      textFormat: Text.PlainText
      color: seg.valueColor
      font.family: seg.fontFamily
      font.pixelSize: seg.fontSize
      renderType: Text.NativeRendering
    }
  }
}
