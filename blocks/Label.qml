import QtQuick
import qs.Commons

// The small spaced capitals over numbers and groups: CPU, MEMORY, CCX 1.
Text {
  property var host: null
  textFormat: Text.PlainText
  elide: Text.ElideRight
  color: host ? host.dim : Color.muted
  font.family: host ? host.fontFamily : Style.font.family
  font.pixelSize: Style.font.caption
  font.capitalization: Font.AllUppercase
  font.letterSpacing: Style.font.caption * 0.18
}
