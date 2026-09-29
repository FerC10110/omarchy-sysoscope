import QtQuick
import QtQuick.Layouts
import qs.Commons

// A section's header: its headline numbers in a row, detail lines on the
// right and the recent history as columns underneath.
ColumnLayout {
  id: hero
  property var block: ({})
  property var host: null
  spacing: Style.space(10)

  RowLayout {
    Layout.fillWidth: true
    spacing: Style.space(28)

    Repeater {
      model: hero.block.blocks || []
      StatBlock {
        required property var modelData
        Layout.alignment: Qt.AlignTop
        Layout.maximumWidth: hero.width * 0.4
        numberWidth: hero.width * 0.4
        block: modelData
        host: hero.host
        showColumns: false
      }
    }

    ColumnLayout {
      Layout.fillWidth: true
      Layout.alignment: Qt.AlignVCenter
      spacing: Style.space(2)
      Repeater {
        model: hero.block.lines || []
        Text {
          required property string modelData
          required property int index
          Layout.fillWidth: true
          text: modelData
          textFormat: Text.PlainText
          elide: Text.ElideRight
          color: hero.host ? (index === 0 ? hero.host.foreground : hero.host.dim) : Color.foreground
          font.family: hero.host ? hero.host.fontFamily : Style.font.family
          font.pixelSize: Style.font.body
        }
      }
    }
  }

  Columns {
    Layout.fillWidth: true
    visible: (hero.block.values || []).length > 1
    implicitHeight: Style.space(44)
    values: hero.block.values || []
    ceiling: (hero.block.blocks || []).length > 0 && hero.block.blocks[0].unit === "%" ? 100 : 0
    color: hero.host ? hero.host.accent : Color.accent
    tipColor: hero.host ? hero.host.accentHi : Color.accent
  }
}
