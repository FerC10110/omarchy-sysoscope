import QtQuick

Item {
  property bool blocked: false
  signal closeRequested()
  signal tabRequested(int direction)
  signal moveRequested(int dx, int dy)
  signal textKey(string text)
  signal activateRequested()
  signal returnRequested()
  signal deleteRequested()
}
