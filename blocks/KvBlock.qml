import QtQuick
import QtQuick.Layouts
import qs.Commons
import "../Icons.js" as Icons

// Key/value rows styled as an Omarchy About card with tree branches and selectable values.
Rectangle {
  id: kv
  property var block: ({})
  property var host: null
  readonly property var rows: block.rows || []


  Layout.fillWidth: true
  implicitHeight: contentCol.implicitHeight + Style.space(24)
  radius: Style.space(10)
  color: kv.host ? kv.host.card : "transparent"
  border.width: 1
  border.color: kv.host ? kv.host.track : "transparent"

  ColumnLayout {
    id: contentCol
    anchors.left: parent.left
    anchors.right: parent.right
    anchors.top: parent.top
    anchors.margins: Style.space(12)
    spacing: Style.space(8)

    // Card Header with icon
    RowLayout {
      Layout.fillWidth: true
      visible: (kv.block.title || "") !== ""
      spacing: Style.space(8)

      Text {
        text: Icons.title(kv.block.title)
        textFormat: Text.PlainText
        color: kv.host ? kv.host.accent : Color.accent
        font.family: kv.host ? kv.host.fontFamily : Style.font.family
        font.pixelSize: Style.font.body
      }

      Label {
        Layout.fillWidth: true
        host: kv.host
        text: kv.block.title || ""
      }
    }

    Rectangle {
      Layout.fillWidth: true
      height: 1
      visible: (kv.block.title || "") !== ""
      color: kv.host ? kv.host.track : "transparent"
    }

    // Rows with tree lines (Omarchy fastfetch style)
    ColumnLayout {
      Layout.fillWidth: true
      spacing: Style.space(4)

      Repeater {
        model: kv.rows
        RowLayout {
          required property var modelData
          required property int index
          Layout.fillWidth: true
          spacing: Style.space(8)

          Text {
            text: index === kv.rows.length - 1 ? "└ " : "├ "
            textFormat: Text.PlainText
            color: kv.host ? kv.host.dim : Color.muted
            font.family: kv.host ? kv.host.fontFamily : Style.font.family
            font.pixelSize: Style.font.bodySmall
            opacity: 0.5
          }

          Text {
            Layout.preferredWidth: Math.max(Style.space(80), kv.width * 0.28)
            text: modelData[0] || ""
            textFormat: Text.PlainText
            elide: Text.ElideRight
            color: kv.host ? kv.host.dim : Color.muted
            font.family: kv.host ? kv.host.fontFamily : Style.font.family
            font.pixelSize: Style.font.bodySmall
          }

          TextEdit {
            Layout.fillWidth: true
            readOnly: true
            selectByMouse: true
            text: modelData[1] || ""
            textFormat: TextEdit.PlainText
            wrapMode: TextEdit.Wrap
            color: kv.host ? kv.host.foreground : Color.foreground
            selectionColor: kv.host ? kv.host.accent : Color.accent
            font.family: kv.host ? kv.host.fontFamily : Style.font.family
            font.pixelSize: Style.font.bodySmall
          }
        }
      }
    }
  }
}

