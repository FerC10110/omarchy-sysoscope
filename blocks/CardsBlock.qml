import QtQuick
import QtQuick.Layouts
import qs.Commons

// Each child block on its own Omarchy card with hover feedback and balanced columns.
GridLayout {
  id: cards
  property var block: ({})
  property var host: null

  readonly property int blockCount: (cards.block.blocks || []).length
  columns: blockCount === 4 && width < Style.space(680) ? 2 : Math.max(1, Math.min(blockCount > 0 ? blockCount : 1, Math.floor(width / Style.space(155))))
  columnSpacing: Style.space(10)
  rowSpacing: Style.space(10)

  Repeater {
    model: cards.block.blocks || []
    Rectangle {
      id: card
      required property var modelData
      Layout.fillWidth: true
      Layout.fillHeight: true
      Layout.preferredWidth: 1
      implicitHeight: inner.implicitHeight + Style.space(26)
      radius: Style.space(10)
      color: cards.host ? (cardMouse.containsMouse && (card.modelData.link || "") !== ""
              ? Qt.tint(cards.host.card, Qt.rgba(cards.host.accent.r, cards.host.accent.g, cards.host.accent.b, 0.08))
              : cards.host.card) : "transparent"
      border.width: 1
      border.color: cardMouse.containsMouse && (card.modelData.link || "") !== ""
              ? (cards.host ? cards.host.accent : Color.accent)
              : (cards.host ? cards.host.track : "transparent")

      Behavior on border.color { ColorAnimation { duration: 150 } }
      Behavior on color { ColorAnimation { duration: 150 } }

      Text {
        anchors.top: parent.top
        anchors.right: parent.right
        anchors.margins: Style.space(10)
        visible: (card.modelData.link || "") !== ""
        text: "›"
        textFormat: Text.PlainText
        color: cardMouse.containsMouse && cards.host ? cards.host.accent : (cards.host ? cards.host.dim : Color.muted)
        font.family: cards.host ? cards.host.fontFamily : Style.font.family
        font.pixelSize: Style.font.body
        font.bold: true
      }

      Loader {
        id: inner
        x: Style.space(12)
        y: Style.space(12)
        width: card.width - Style.space(24)
        source: "Blocks.qml"
        onLoaded: {
          item.width = Qt.binding(function() { return inner.width })
          item.host = Qt.binding(function() { return cards.host })
          item.blocks = Qt.binding(function() { return [card.modelData] })
        }
      }

      MouseArea {
        id: cardMouse
        anchors.fill: parent
        hoverEnabled: true
        enabled: (card.modelData.link || "") !== ""
        cursorShape: enabled ? Qt.PointingHandCursor : Qt.ArrowCursor
        onClicked: if (cards.host) cards.host.follow(card.modelData.link)
      }
    }
  }
}

