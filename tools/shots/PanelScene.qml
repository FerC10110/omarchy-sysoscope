import QtQuick
import "../.." as Plugin

Item {
  property var service: null
  property string section: "summary"
  property string look: ""
  anchors.fill: parent
  Plugin.Panel {
    id: panel
    service: parent.service
  }
  onSectionChanged: panel.select(section)
}
