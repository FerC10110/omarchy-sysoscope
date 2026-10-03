import QtQuick
import QtQuick.Layouts
import qs.Commons
import "../Icons.js" as Icons

// A section's header: Omarchy About-inspired hardware card with structured
// tree lines, icons, and sparkline columns.
Rectangle {
  id: hero
  property var block: ({})
  property var host: null

  readonly property var statBlocks: block.blocks || []
  readonly property var lines: block.lines || []
  readonly property var values: block.values || []
  function t(s) { return host ? host.t(s) : s }
  readonly property bool isMachineSummary: statBlocks.length === 1 && !statBlocks[0].label && !statBlocks[0].unit


  Layout.fillWidth: true
  implicitHeight: cardLayout.implicitHeight + Style.space(24)
  radius: Style.space(10)
  color: hero.host ? hero.host.card : "transparent"
  border.width: 1
  border.color: hero.host ? hero.host.track : "transparent"

  ColumnLayout {
    id: cardLayout
    anchors.left: parent.left
    anchors.right: parent.right
    anchors.top: parent.top
    anchors.margins: Style.space(12)
    spacing: Style.space(10)

    // Machine summary header (Omarchy About style)
    RowLayout {
      Layout.fillWidth: true
      visible: hero.isMachineSummary
      spacing: Style.space(12)

      Rectangle {
        width: Style.space(42)
        height: Style.space(42)
        radius: Style.space(8)
        color: hero.host ? Qt.rgba(hero.host.accent.r, hero.host.accent.g, hero.host.accent.b, 0.12) : "transparent"
        border.width: 1
        border.color: hero.host ? Qt.rgba(hero.host.accent.r, hero.host.accent.g, hero.host.accent.b, 0.3) : "transparent"

        Text {
          anchors.centerIn: parent
          text: ""
          textFormat: Text.PlainText
          color: hero.host ? hero.host.accent : Color.accent
          font.family: hero.host ? hero.host.fontFamily : Style.font.family
          font.pixelSize: Style.font.title
        }
      }

      ColumnLayout {
        Layout.fillWidth: true
        spacing: Style.space(2)

        RowLayout {
          Layout.fillWidth: true
          spacing: Style.space(8)

          Text {
            text: hero.statBlocks.length > 0 ? (hero.statBlocks[0].text || hero.t("System")) : hero.t("System")
            textFormat: Text.PlainText
            color: hero.host ? hero.host.foreground : Color.foreground
            font.family: hero.host ? hero.host.fontFamily : Style.font.family
            font.pixelSize: Style.font.title
            font.bold: true
          }

          Rectangle {
            implicitWidth: statusText.implicitWidth + Style.space(10)
            implicitHeight: statusText.implicitHeight + Style.space(4)
            radius: Style.space(4)
            color: hero.host ? hero.host.track : "transparent"
            Text {
              id: statusText
              anchors.centerIn: parent
              text: hero.t("ONLINE")
              textFormat: Text.PlainText
              color: hero.host ? hero.host.accent : Color.accent
              font.family: hero.host ? hero.host.fontFamily : Style.font.family
              font.pixelSize: Style.font.caption
              font.bold: true
              font.letterSpacing: 1
            }
          }

          Item { Layout.fillWidth: true }
        }

        Text {
          text: hero.t("Omarchy Linux · Hardware Summary")
          textFormat: Text.PlainText
          color: hero.host ? hero.host.dim : Color.muted
          font.family: hero.host ? hero.host.fontFamily : Style.font.family
          font.pixelSize: Style.font.caption
        }
      }
    }

    Rectangle {
      Layout.fillWidth: true
      height: 1
      visible: hero.isMachineSummary
      color: hero.host ? hero.host.track : "transparent"
    }

    // Machine detail tree lines (Omarchy fastfetch style)
    ColumnLayout {
      Layout.fillWidth: true
      visible: hero.isMachineSummary
      spacing: Style.space(4)

      Repeater {
        model: hero.lines
        RowLayout {
          required property string modelData
          required property int index
          Layout.fillWidth: true
          spacing: Style.space(6)

          Text {
            text: index === hero.lines.length - 1 ? "└ └" : "│ ├"
            textFormat: Text.PlainText
            color: hero.host ? hero.host.dim : Color.muted
            font.family: hero.host ? hero.host.fontFamily : Style.font.family
            font.pixelSize: Style.font.bodySmall
            opacity: 0.6
          }

          Text {
            text: Icons.heroLine(modelData)
            textFormat: Text.PlainText
            color: hero.host ? hero.host.accent : Color.accent
            font.family: hero.host ? hero.host.fontFamily : Style.font.family
            font.pixelSize: Style.font.bodySmall
          }

          Text {
            Layout.fillWidth: true
            text: modelData
            textFormat: Text.PlainText
            elide: Text.ElideRight
            color: index === 0 && hero.host ? hero.host.foreground : (hero.host ? hero.host.dim : Color.muted)
            font.family: hero.host ? hero.host.fontFamily : Style.font.family
            font.pixelSize: Style.font.bodySmall
          }
        }
      }
    }

    // Standard metric hero (e.g. CPU or Memory view)
    RowLayout {
      Layout.fillWidth: true
      visible: !hero.isMachineSummary
      spacing: Style.space(20)

      Repeater {
        model: hero.statBlocks
        StatBlock {
          required property var modelData
          Layout.alignment: Qt.AlignTop
          Layout.maximumWidth: hero.width * 0.45
          numberWidth: hero.width * 0.45
          block: modelData
          host: hero.host
          showColumns: false
        }
      }

      ColumnLayout {
        Layout.fillWidth: true
        Layout.alignment: Qt.AlignVCenter
        spacing: Style.space(3)
        Repeater {
          model: hero.lines
          RowLayout {
            required property string modelData
            required property int index
            Layout.fillWidth: true
            spacing: Style.space(6)

            Text {
              text: index === hero.lines.length - 1 ? "└" : "├"
              textFormat: Text.PlainText
              color: hero.host ? hero.host.dim : Color.muted
              font.family: hero.host ? hero.host.fontFamily : Style.font.family
              font.pixelSize: Style.font.bodySmall
              opacity: 0.6
            }

            Text {
              Layout.fillWidth: true
              text: modelData
              textFormat: Text.PlainText
              elide: Text.ElideRight
              color: hero.host ? (index === 0 ? hero.host.foreground : hero.host.dim) : Color.foreground
              font.family: hero.host ? hero.host.fontFamily : Style.font.family
              font.pixelSize: Style.font.bodySmall
            }
          }
        }
      }
    }

    Columns {
      Layout.fillWidth: true
      visible: hero.values.length > 1
      implicitHeight: Style.space(40)
      values: hero.values
      ceiling: hero.statBlocks.length > 0 && hero.statBlocks[0].unit === "%" ? 100 : 0
      color: hero.host ? hero.host.accent : Color.accent
      tipColor: hero.host ? hero.host.accentHi : Color.accent
    }
  }
}

