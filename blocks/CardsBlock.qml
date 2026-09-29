import QtQuick
import QtQuick.Layouts
import qs.Commons

// Each child block on its own card, as many per row as fit at ~170 px. A
// card with a link opens that section.
GridLayout {
  id: cards
  property var block: ({})
  property var host: null
  columns: Math.max(1, Math.floor(width / Style.space(170)))
  columnSpacing: Style.space(8)
  rowSpacing: Style.space(8)

  Repeater {
    model: cards.block.blocks || []
    Rectangle {
      id: card
      required property var modelData
      Layout.fillWidth: true
      Layout.fillHeight: true
      Layout.preferredWidth: 1
      implicitHeight: inner.implicitHeight + Style.space(24)
      radius: Style.space(8)
      color: cards.host ? cards.host.card : "transparent"
      border.width: 1
      border.color: cards.host ? cards.host.track : "transparent"

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
        anchors.fill: parent
        enabled: (card.modelData.link || "") !== ""
        cursorShape: enabled ? Qt.PointingHandCursor : Qt.ArrowCursor
        onClicked: if (cards.host) cards.host.follow(card.modelData.link)
      }
    }
  }
}
