import QtQuick
import QtQuick.Layouts
import qs.Commons
import "Settings.js" as Settings

// The Settings section of the panel: one row per setting, changed with the
// keys (or a click, which is Space). What the bar shows is saved in the bar's
// entry of shell.json; the collector's settings in config.json; the language
// in the file every plugin shares.
ColumnLayout {
  id: view
  property var host: null            // the panel
  property int index: 0
  property bool typing: false        // no text fields here

  readonly property var service: host ? host.service : null
  readonly property var widget: host ? host.hostWidget : null
  readonly property var bar: widget && widget.settings ? widget.settings : ({})
  readonly property var rows: service
    ? Settings.rows(bar, service.config, service.language, service.snapshot, host.t)
    : []

  // A ListModel updated in place: a new JS array every tick (the cost row
  // changes each sample) would reset the list and scroll it to the top.
  ListModel { id: rowModel }
  property var shownRows: []
  function sync() {
    var next = rows
    if (Settings.sameRows(shownRows, next)) {
      for (var i = 0; i < next.length; i++) rowModel.set(i, next[i])
    } else {
      rowModel.clear()
      for (var j = 0; j < next.length; j++) rowModel.append(next[j])
    }
    shownRows = next
  }
  onRowsChanged: sync()
  Component.onCompleted: sync()

  function move(dy) { index = Math.max(0, Math.min(rows.length - 1, index + dy)) }

  // key: " ", "h", "l", "K", "J", "s" or "o". Returns false when the key is
  // not a settings key, so the panel can use it.
  function handleKey(key) {
    if (!service || rows.length === 0) return false
    var r = Settings.apply(rows[index], key, bar, service.config, service.language, service.snapshot)
    if (!r.bar && !r.config && !r.language) return ["K", "J", "s", "o", "h", "l", " "].indexOf(key) >= 0
    if (r.bar && widget) widget.persist(r.bar)
    if (r.config) service.saveConfig(r.config)
    if (r.language) service.setLanguage(r.language)
    if (key === "K" || key === "J") {
      // Follow the row that moved.
      var id = rows[index].id
      Qt.callLater(function() { for (var i = 0; i < view.rows.length; i++) if (view.rows[i].id === id) view.index = i })
    }
    return true
  }

  spacing: Style.space(6)

  ListView {
    id: list
    Layout.fillWidth: true
    Layout.fillHeight: true
    clip: true
    model: rowModel
    currentIndex: view.index
    boundsBehavior: Flickable.StopAtBounds
    highlightMoveDuration: 0
    spacing: Style.space(1)

    section.property: "group"
    section.delegate: Text {
      required property string section
      topPadding: Style.space(8)
      bottomPadding: Style.space(2)
      text: section
      textFormat: Text.PlainText
      color: view.host ? view.host.dim : Color.muted
      font.family: view.host ? view.host.fontFamily : Style.font.family
      font.pixelSize: Style.font.caption
      font.capitalization: Font.AllUppercase
      font.letterSpacing: Style.font.caption * 0.18
    }

    delegate: Rectangle {
      required property string label
      required property string value
      required property string detail
      required property int index
      readonly property bool selected: index === view.index
      width: list.width
      implicitHeight: line.implicitHeight + Style.space(6)
      radius: Style.space(6)
      color: selected && view.host ? view.host.card : "transparent"

      RowLayout {
        id: line
        anchors.fill: parent
        anchors.leftMargin: Style.space(8)
        anchors.rightMargin: Style.space(8)
        spacing: Style.space(12)
        Text {
          Layout.preferredWidth: Style.space(230)
          text: label
          textFormat: Text.PlainText
          elide: Text.ElideRight
          color: selected && view.host ? view.host.accent : (view.host ? view.host.foreground : Color.foreground)
          font.family: view.host ? view.host.fontFamily : Style.font.family
          font.pixelSize: Style.font.body
        }
        Text {
          Layout.preferredWidth: Style.space(160)
          Layout.fillWidth: detail === ""
          text: value
          textFormat: Text.PlainText
          elide: Text.ElideRight
          color: view.host ? view.host.accent : Color.accent
          font.family: view.host ? view.host.fontFamily : Style.font.family
          font.pixelSize: Style.font.body
        }
        Text {
          Layout.fillWidth: true
          visible: detail !== ""
          text: detail
          textFormat: Text.PlainText
          elide: Text.ElideRight
          color: view.host ? view.host.dim : Color.muted
          font.family: view.host ? view.host.fontFamily : Style.font.family
          font.pixelSize: Style.font.bodySmall
        }
      }

      MouseArea {
        anchors.fill: parent
        cursorShape: Qt.PointingHandCursor
        onClicked: { view.index = index; view.handleKey(" ") }
      }
    }
  }

  Text {
    Layout.fillWidth: true
    text: view.host ? view.host.t("j/k choose · Space show or change · h/l less/more · J/K move · s style · o only above threshold") : ""
    textFormat: Text.PlainText
    wrapMode: Text.Wrap
    color: view.host ? view.host.dim : Color.muted
    font.family: view.host ? view.host.fontFamily : Style.font.family
    font.pixelSize: Style.font.bodySmall
  }
}
