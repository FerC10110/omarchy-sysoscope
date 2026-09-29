pragma Singleton
import QtQuick

QtObject {
  function space(px) { return Math.round(px) }
  property int gapsOut: 8
  property string fontFamily: "JetBrainsMono Nerd Font"
  readonly property QtObject font: QtObject {
    property string family: "JetBrainsMono Nerd Font"
    property int caption: 10
    property int bodySmall: 11
    property int body: 12
    property int subtitle: 13
    property int title: 14
    property int display: 24
    property int displayLarge: 28
  }
}
