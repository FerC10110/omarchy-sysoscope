import QtQuick
import qs.Commons

// Draws its content at the requested size on the panel background.
Rectangle {
  property var anchorItem: null
  property var owner: null
  property var bar: null
  property bool open: true
  property var focusTarget: null
  property real contentWidth: 860
  property real contentHeight: 620
  property int gap: 8
  property int padding: 16
  readonly property real screenW: 0
  readonly property real screenH: 0
  readonly property real availableCardWidth: 0
  readonly property real availableCardHeight: 0
  function fittedContentWidth(w) { return w }
  function fittedContentHeight(h, max) { return h }
  function cappedContentHeight(h) { return h }
  width: contentWidth + padding * 2
  height: contentHeight + padding * 2
  color: Color.background
  border.width: 2
  border.color: Color.accent
  default property alias content: inner.data
  Item { id: inner; anchors.fill: parent; anchors.margins: 16 }
}
