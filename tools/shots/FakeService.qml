import QtQuick

// Serves the views written by `go test ./internal/view -run TestWriteShots`
// from SHOTS_DATA, the way Service.qml serves the stream.
QtObject {
  id: svc
  property string dir: ""
  property string focused: "summary"
  function load(name) {
    var x = new XMLHttpRequest()
    x.open("GET", "file://" + dir + "/" + name, false)
    x.send()
    return JSON.parse(x.responseText)
  }
  readonly property string language: dir.slice(-2)
  readonly property var sections: load("sections.json")
  readonly property var snapshot: load("snapshot.json")
  readonly property var view: focused === "" ? null : load(focused + ".json")
  property var config: ({ interval: 2, units: "c", cards: ["cpu", "memory", "gpu", "storage", "network"], span: "2m", section: "summary" })
  property string problem: ""
  property var recent: ({})
  property var series: ({})
  function setFocus(id, who) { focused = id }
  function rememberSection(id) {}
  property var lastResult: null
  function runAction(id, row, input) {}
  function saveConfig(c) {}
  function setLanguage(l) {}
}
