import QtQuick
import qs.Commons
import "../Theme.js" as Theme

// Recent values as columns, newest on the right, with a gradient from `tipColor`
// (at the tip) to `color`. `ceiling` fixes the scale (100 for percentages).
Item {
  id: cols
  property var values: []
  property real ceiling: 0
  property int maxColumns: 60
  property color color: Color.accent
  property color tipColor: color
  readonly property var scaled: Theme.columns(values, maxColumns, ceiling)
  readonly property real step: width / maxColumns
  implicitHeight: Style.space(32)

  Repeater {
    model: cols.scaled.shown.length
    Rectangle {
      required property int index
      x: cols.width - (cols.scaled.shown.length - index) * cols.step
      width: Math.max(1, cols.step - Style.space(2))
      height: Math.max(Style.space(2), cols.scaled.shown[index] / cols.scaled.top * cols.height)
      y: cols.height - height
      radius: Math.min(Style.space(2), width / 2)
      gradient: Gradient {
        GradientStop { position: 0; color: cols.tipColor }
        GradientStop { position: 1; color: cols.color }
      }
      Behavior on height { NumberAnimation { duration: 250; easing.type: Easing.OutCubic } }
    }
  }
}
