import QtQuick
import QtQuick.Effects
import qs.Commons
import "../.." as Plugin
import "../../BarModel.js" as BarModel

// The Market's preview, 1920×1080: a bar across the top with Sysoscope's
// segments, the name and what it does on the left, and the real panel on the
// Summary on the right. What matters sits in the middle, since the Market's
// card crops the top and bottom.
Item {
  id: scene
  property var service: null
  property string section: "preview"
  property string look: "detailed"
  width: 1920
  height: 1080

  function wave(base, amp) {
    var out = []
    for (var i = 0; i < 14; i++) out.push(base + amp * Math.sin(i / 2))
    return out
  }
  readonly property var recent: ({ cpu: wave(30, 15), ram: wave(30, 2), gpu: wave(15, 8), net: wave(20000, 15000) })
  readonly property string font: "JetBrainsMono Nerd Font"

  Rectangle {
    anchors.fill: parent
    gradient: Gradient {
      GradientStop { position: 0; color: Qt.lighter(Color.background, 1.35) }
      GradientStop { position: 1; color: Color.background }
    }
  }

  // The bar.
  Rectangle {
    id: bar
    width: parent.width
    height: 48
    color: Qt.darker(Color.background, 1.2)
    Row {
      anchors.left: parent.left
      anchors.leftMargin: 40
      anchors.verticalCenter: parent.verticalCenter
      spacing: 22
      Repeater {
        model: scene.service
          ? BarModel.segments(scene.service.snapshot, { show: ["cpu", "cputemp", "ram", "gpu", "gputemp", "disk"], styles: { cpu: "spark", ram: "gauge" } }, scene.recent, "c")
          : []
        Plugin.BarSegment {
          required property var modelData
          segment: modelData
          look: "detailed"
          fontFamily: scene.font
          fontSize: 17
          barHeight: 32
        }
      }
    }
  }

  // The name and what it does.
  Column {
    x: 110
    anchors.verticalCenter: parent.verticalCenter
    anchors.verticalCenterOffset: 24
    width: 640
    spacing: 22

    Text {
      text: "Sysoscope"
      textFormat: Text.PlainText
      color: Color.accent
      font.family: scene.font
      font.pixelSize: 76
      font.bold: true
      layer.enabled: true
      layer.effect: MultiEffect { shadowEnabled: true; shadowColor: Color.accent; shadowOpacity: 0.3; shadowBlur: 0.7; shadowHorizontalOffset: 0; shadowVerticalOffset: 0 }
    }
    Text {
      width: parent.width
      text: "Your whole machine, one click from the bar."
      textFormat: Text.PlainText
      wrapMode: Text.Wrap
      color: Color.foreground
      font.family: scene.font
      font.pixelSize: 30
    }
    Item { width: 1; height: 14 }
    Repeater {
      model: [
        "12 readings in the bar, detailed or minimal",
        "CPU, GPU, disks, network, sensors and more",
        "Every missing value says why",
        "Follows your Omarchy theme",
        "Under 4 ms per sample, 13 MB of memory"
      ]
      Row {
        required property string modelData
        spacing: 16
        Rectangle { width: 10; height: 10; radius: 5; color: Color.accent; anchors.verticalCenter: parent.verticalCenter }
        Text {
          text: modelData
          textFormat: Text.PlainText
          color: Qt.rgba(Color.foreground.r, Color.foreground.g, Color.foreground.b, 0.85)
          font.family: scene.font
          font.pixelSize: 23
        }
      }
    }
  }

  // The panel, on the Summary.
  Item {
    id: frame
    x: 800
    anchors.verticalCenter: parent.verticalCenter
    anchors.verticalCenterOffset: 24
    width: 1040
    // The panel is 900×670, scaled to fill the width.
    height: 670 * width / 900

    Rectangle {
      id: card
      anchors.fill: parent
      radius: 4
      color: Color.background
      layer.enabled: true
      layer.effect: MultiEffect { shadowEnabled: true; shadowColor: "black"; shadowOpacity: 0.6; shadowBlur: 1.0; shadowVerticalOffset: 12 }
    }
    Item {
      anchors.fill: parent
      clip: true
      Item {
        width: 900
        height: 670
        scale: parent.width / 900
        transformOrigin: Item.TopLeft
        Plugin.Panel {
          id: panel
          service: scene.service
        }
      }
    }
  }
  Component.onCompleted: panel.select("summary")
}
