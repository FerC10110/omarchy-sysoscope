import QtQuick

// Stand-in for the shell's Panel: always open, no IPC.
Item {
  property string moduleName: ""
  property string ipcTarget: ""
  property bool manageIpc: false
  property var bar: null
  property bool opened: true
  property bool popoutSwitchClosing: false
  function open() { opened = true }
  function close() {}
  function closeForPopoutSwitch() {}
  function switchPanel(direction) {}
}
