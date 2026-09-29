import QtQuick

Item {
  property string title: ""
  property string meta: ""
  property color foreground: "white"
  property string fontFamily: ""
  implicitHeight: 40
  Text { text: parent.title; textFormat: Text.PlainText; color: parent.foreground; font.pixelSize: 14 }
}
