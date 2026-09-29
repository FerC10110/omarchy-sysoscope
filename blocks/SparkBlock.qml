import QtQuick
import QtQuick.Layouts
import qs.Commons

// A label, a text and the recent values as columns.
ColumnLayout {
  id: spark
  property var block: ({})
  property var host: null
  readonly property var values: block.values || []
  spacing: Style.space(3)

  RowLayout {
    Layout.fillWidth: true
    visible: (spark.block.label || "") !== "" || (spark.block.text || "") !== ""
    Text {
      Layout.fillWidth: true
      text: spark.block.label || ""
      textFormat: Text.PlainText
      color: spark.host ? spark.host.foreground : Color.foreground
      font.family: spark.host ? spark.host.fontFamily : Style.font.family
      font.pixelSize: Style.font.body
    }
    Text {
      text: spark.block.text || ""
      textFormat: Text.PlainText
      color: spark.host ? spark.host.dim : Color.muted
      font.family: spark.host ? spark.host.fontFamily : Style.font.family
      font.pixelSize: Style.font.body
    }
  }

  Columns {
    Layout.fillWidth: true
    visible: spark.values.length > 1
    implicitHeight: Style.space(32)
    values: spark.values
    color: spark.host ? spark.host.accent : Color.accent
    tipColor: spark.host ? spark.host.accentHi : Color.accent
  }
}
