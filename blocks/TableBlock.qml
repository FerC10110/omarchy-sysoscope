import QtQuick
import QtQuick.Layouts
import qs.Commons

// A table: dim headers, then the rows (the first 50).
ColumnLayout {
  id: table
  property var block: ({})
  property var host: null
  readonly property int limit: 50
  readonly property var columns: block.columns || []
  readonly property var rows: block.rows || []
  readonly property var shown: rows.slice(0, limit)
  spacing: Style.space(4)

  Label { visible: text !== ""; host: table.host; text: table.block.title || "" }

  GridLayout {
    Layout.fillWidth: true
    columns: Math.max(1, table.columns.length)
    columnSpacing: Style.space(14)
    rowSpacing: Style.space(3)

    Repeater {
      model: table.columns.length * (table.shown.length + 1)
      Text {
        id: cellText
        required property int index
        readonly property int row: Math.floor(index / table.columns.length) - 1
        readonly property int col: index % table.columns.length
        // Every column can shrink (and elide) so a long cell, like a list of
        // mount points, cannot push the others out of the panel.
        Layout.fillWidth: true
        Layout.preferredWidth: implicitWidth
        Layout.maximumWidth: Math.max(Style.space(40), table.width * 0.45)
        text: row < 0 ? table.columns[col] : (table.shown[row].cells[col] || "")
        textFormat: Text.PlainText
        elide: Text.ElideRight
        color: table.host ? (row < 0 ? table.host.dim : table.host.foreground) : Color.foreground
        font.family: table.host ? table.host.fontFamily : Style.font.family
        font.pixelSize: row < 0 ? Style.font.caption : Style.font.body
        font.capitalization: row < 0 ? Font.AllUppercase : Font.MixedCase
        font.letterSpacing: row < 0 ? Style.font.caption * 0.18 : 0
        // The picked row (Applications): a band behind its cells, widened by
        // the column spacing so it reads as one row. Clicking a row picks it.
        readonly property bool picked: row >= 0 && !!table.block.selectable && !!table.host
                                       && table.host.selectedRow !== undefined && table.host.selectedRow !== ""
                                       && table.shown[row].id === table.host.selectedRow
        // A search opened this row: scroll the panel to it once it is drawn
        // (later, so the layout has placed it).
        readonly property bool revealed: row >= 0 && col === 0 && !!table.host && !!table.host.revealRow
                                         && table.shown[row].id === table.host.revealRow
        function revealLater() {
          if (revealed && table.host.reveal) Qt.callLater(function() { table.host.reveal(cellText) })
        }
        onRevealedChanged: revealLater()
        // The layout has not placed this cell yet the first time it exists;
        // retry once it does (y is set directly by GridLayout, not anchors).
        onYChanged: revealLater()
        Component.onCompleted: revealLater()
        Rectangle {
          z: -1
          anchors.fill: parent
          anchors.leftMargin: -Style.space(7)
          anchors.rightMargin: -Style.space(7)
          anchors.topMargin: -Style.space(1)
          anchors.bottomMargin: -Style.space(1)
          visible: parent.picked
          color: table.host ? table.host.track : "transparent"
        }
        MouseArea {
          anchors.fill: parent
          enabled: parent.row >= 0 && !!table.block.selectable && !!table.host && !!table.host.rows && table.host.rows.length > 0
          // While a question is open the pick stays on the row it is about.
          onClicked: if (!table.host.ask) table.host.selectedRow = table.shown[parent.row].id
        }
      }
    }
  }

  Text {
    visible: table.rows.length > table.limit
    text: table.host ? table.host.t("… %1 more", [table.rows.length - table.limit]) : ""
    textFormat: Text.PlainText
    color: table.host ? table.host.dim : Color.muted
    font.family: table.host ? table.host.fontFamily : Style.font.family
    font.pixelSize: Style.font.bodySmall
  }
}
