import QtQuick
import qs.Commons
import "../.." as Plugin
import "../../BarModel.js" as BarModel

// A fake 900×40 bar with the segments of the snapshot; look "both" draws the
// minimal bar over the detailed one.
Item {
  id: scene
  property var service: null
  property string section: "bar"
  property string look: "detailed"
  width: 900
  height: looks.length * 40

  function wave(base, amp) {
    var out = []
    for (var i = 0; i < 14; i++) out.push(base + amp * Math.sin(i / 2))
    return out
  }
  readonly property var recent: ({ cpu: wave(30, 15), cputemp: wave(45, 5), ram: wave(30, 2), gpu: wave(15, 8), net: wave(20000, 15000) })
  readonly property var looks: look === "both" ? ["minimal", "detailed"] : [look]

  Column {
    Repeater {
      model: scene.looks
      Rectangle {
        required property string modelData
        width: scene.width
        height: 40
        color: Color.background
        Row {
          anchors.centerIn: parent
          spacing: Style.space(12)
          Repeater {
            model: scene.service
              ? BarModel.segments(scene.service.snapshot, { show: ["cpu", "cputemp", "ram", "gpu", "net", "disk"], styles: { cpu: "spark", ram: "gauge" } }, scene.recent, "c")
              : []
            Plugin.BarSegment {
              required property var modelData
              segment: modelData
              look: parent.parent.modelData
              fontSize: Style.font.body
              barHeight: 30
            }
          }
        }
      }
    }
  }
}
