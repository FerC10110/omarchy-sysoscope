import QtQuick
import QtQuick.Layouts
import qs.Commons

// Key/value rows. The values can be selected, to copy a serial or an address.
ColumnLayout {
  id: kv
  property var block: ({})
  property var host: null
  readonly property var rows: block.rows || []
  spacing: Style.space(4)

  Label { visible: text !== ""; host: kv.host; text: kv.block.title || "" }

  GridLayout {
    Layout.fillWidth: true
    columns: 2
    columnSpacing: Style.space(14)
    rowSpacing: Style.space(3)

    Repeater {
      model: kv.rows.length * 2
      Loader {
        required property int index
        readonly property bool isKey: index % 2 === 0
        readonly property string value: kv.rows[Math.floor(index / 2)][isKey ? 0 : 1]
        Layout.fillWidth: !isKey
        Layout.alignment: Qt.AlignTop
        sourceComponent: isKey ? keyText : valueText
        onLoaded: item.text = Qt.binding(function() { return value })
      }
    }
  }

  Component {
    id: keyText
    Text {
      textFormat: Text.PlainText
      color: kv.host ? kv.host.dim : Color.muted
      font.family: kv.host ? kv.host.fontFamily : Style.font.family
      font.pixelSize: Style.font.body
    }
  }

  Component {
    id: valueText
    TextEdit {
      readOnly: true
      selectByMouse: true
      textFormat: TextEdit.PlainText
      wrapMode: TextEdit.Wrap
      color: kv.host ? kv.host.foreground : Color.foreground
      selectionColor: kv.host ? kv.host.accent : Color.accent
      font.family: kv.host ? kv.host.fontFamily : Style.font.family
      font.pixelSize: Style.font.body
    }
  }
}
