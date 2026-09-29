import QtQuick
import QtQuick.Layouts
import qs.Commons

// A list of view blocks from the collector (sysoscope view), each drawn by
// its own file. A type this plugin does not know draws nothing, so a newer
// collector does not break an older panel.
ColumnLayout {
  id: list
  property var blocks: []
  property var host: null

  readonly property var sources: ({
    kv: "KvBlock.qml", meter: "MeterBlock.qml", spark: "SparkBlock.qml", table: "TableBlock.qml",
    text: "TextBlock.qml", group: "GroupBlock.qml", cores: "CoresBlock.qml",
    stat: "StatBlock.qml", cards: "CardsBlock.qml", hero: "HeroBlock.qml"
  })

  spacing: Style.space(10)

  Repeater {
    model: list.blocks
    Loader {
      required property var modelData
      Layout.fillWidth: true
      active: list.sources[modelData.type] !== undefined
      source: active ? list.sources[modelData.type] : ""
      onLoaded: { item.host = Qt.binding(function() { return list.host }); item.block = modelData }
      onModelDataChanged: if (item) item.block = modelData
    }
  }
}
