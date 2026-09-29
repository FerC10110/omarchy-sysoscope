import QtQuick
import QtQuick.Layouts
import qs.Commons

// A line of text. A warning or a critical one is a stripe: card background
// and a coloured left edge. With a link, a click (or Enter on the panel)
// opens that section.
Item {
  id: line
  property var block: ({})
  property var host: null
  readonly property bool linked: (block.link || "") !== ""
  readonly property bool stripe: block.level === "warn" || block.level === "crit"
  implicitHeight: label.implicitHeight + (stripe ? Style.space(14) : 0)

  Rectangle {
    anchors.fill: parent
    visible: line.stripe
    radius: Style.space(4)
    color: line.host ? line.host.card : "transparent"
    Rectangle {
      width: Style.space(3)
      height: parent.height
      radius: Style.space(2)
      color: line.host ? line.host.strongColor(line.block.level) : Color.accent
    }
  }

  Text {
    id: label
    anchors.left: parent.left
    anchors.right: parent.right
    anchors.verticalCenter: parent.verticalCenter
    anchors.leftMargin: line.stripe ? Style.space(12) : 0
    anchors.rightMargin: line.stripe ? Style.space(10) : 0
    text: (line.block.text || "") + (line.linked ? "  ›" : "")
    textFormat: Text.PlainText
    wrapMode: Text.Wrap
    color: !line.host ? Color.foreground
         : (line.block.level || "") === "" ? line.host.dim : line.host.levelColor(line.block.level, false)
    font.family: line.host ? line.host.fontFamily : Style.font.family
    font.pixelSize: Style.font.body
    font.underline: line.linked && mouse.containsMouse
  }

  MouseArea {
    id: mouse
    anchors.fill: parent
    enabled: line.linked
    hoverEnabled: true
    cursorShape: Qt.PointingHandCursor
    onClicked: if (line.host) line.host.follow(line.block.link)
  }
}
