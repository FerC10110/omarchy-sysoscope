import QtQuick
import qs.Commons
import qs.Ui
import "I18n.js" as I18n
import "BarModel.js" as BarModel

// Bar entry point for Sysoscope: the plugin icon and the metrics the user
// chose, each one opening its section of the panel. The collector runs in
// Service.qml (one instance); this widget exists once per monitor and only
// shows it. Middle click refreshes, right click opens btop.
BarWidget {
  id: root
  moduleName: "io.github.ferc10110.sysoscope"

  // Looked up on a timer rather than bound: the service is created
  // asynchronously and can be rebuilt on a plugin reload.
  property var service: null

  readonly property var panel: panelLoader.item
  readonly property bool opened: panel ? panel.opened === true : false
  readonly property bool popoutSwitchClosing: panel ? panel.popoutSwitchClosing === true : false

  readonly property string language: service ? service.language : "en"
  function t(s, args) { return I18n.t(s, root.language, args) }

  readonly property string problem: service ? service.problem : ""
  readonly property var barSettings: ({
    show: setting("show", null),
    styles: setting("styles", ({})),
    onlyOver: setting("onlyOver", []),
    thresholds: setting("thresholds", ({})),
    showIcon: setting("showIcon", true),
    look: setting("look", "detailed")
  })
  readonly property string look: BarModel.look(barSettings)
  // The detailed bar is sized by its segments, not by the joined text.
  readonly property bool detailed: !vertical && look !== "minimal"
  readonly property var history: service ? (service.config.span === "2m" ? service.recent : service.series) : ({})
  readonly property var segments: problem !== "" || !service || !service.snapshot
    ? [{ key: "icon", text: BarModel.PLUGIN_ICON, urgent: false, section: "summary", tooltip: problem || "Sysoscope" }]
    : BarModel.segments(service.snapshot, barSettings, history, service.config.units, root.t)
  readonly property string joined: segments.map(function(s) { return s.text }).join("  ")

  // Keys the bar shows, so the service knows which histories to fetch.
  onSegmentsChanged: if (service && BarModel.isLeader(bar ? bar.moduleWidgets(moduleName) : [], root))
    service.barKeys = segments.map(function(s) { return s.key })

  // The icon and toggle reopen the section the user was last on.
  readonly property string lastSection: service && service.config.section ? service.config.section : "summary"
  function open() { if (panel) panel.openSection(root.lastSection) }
  function close() { if (panel) panel.close() }
  function togglePanel() { if (!panel) return; if (panel.opened) panel.close(); else panel.openSection(root.lastSection) }
  function closeForPopoutSwitch() { if (panel) panel.closeForPopoutSwitch() }
  function openSection(id) {
    if (!panel) return
    if (panel.opened && panel.section === id) panel.close()
    else panel.openSection(id)
  }

  // SettingsView writes through this: updateEntryInline replaces the whole
  // entry, so the other settings are copied in.
  function persist(changes) {
    if (bar && bar.shell) bar.shell.updateEntryInline(moduleName, BarModel.mergeEntry(moduleName, settings, changes))
  }

  function lookupService() {
    var found = bar && bar.shell ? bar.shell.serviceFor(moduleName) : null
    if (found !== service) service = found
  }

  function injectPanel() {
    var target = panelLoader.item
    if (!target) return
    if ("bar" in target) target.bar = root.bar
    if ("settings" in target) target.settings = root.settings
    if ("anchorItem" in target) target.anchorItem = anchorProxy
    if ("hostWidget" in target) target.hostWidget = root
    if ("service" in target) target.service = root.service
  }

  implicitWidth: vertical ? button.implicitWidth : button.implicitWidth
  implicitHeight: vertical ? Math.max(button.implicitHeight, column.implicitHeight + Style.space(12)) : button.implicitHeight

  onBarChanged: { lookupService(); injectPanel() }
  onSettingsChanged: injectPanel()
  onServiceChanged: injectPanel()

  Timer {
    interval: 1000
    repeat: true
    running: true
    triggeredOnStart: true
    onTriggered: root.lookupService()
  }

  Connections {
    target: root.service
    function onPanelRequested(action, section) {
      if (BarModel.panelTarget(root.bar, root.moduleName) !== root || !root.panel) return
      if (action === "toggle") root.togglePanel()
      else if (action === "open") root.panel.openSection(section)
    }
  }

  Loader {
    id: panelLoader
    active: true
    source: Qt.resolvedUrl("Panel.qml")
    visible: false
    onLoaded: {
      root.injectPanel()
      Qt.callLater(root.injectPanel)
    }
  }

  // The button gives the slot its size, the tooltip on the empty edges and
  // the middle and right clicks; the segments are drawn on top of it so each
  // one can have its own colour, tooltip and left click.
  WidgetButton {
    id: button
    anchors.fill: parent
    bar: root.bar
    text: root.vertical || root.detailed ? " " : root.joined
    fixedWidth: root.detailed ? segRow.implicitWidth + scaledHorizontalMargin * 2 : 0
    labelVisible: false
    dimmed: root.problem !== ""
    tooltipText: root.problem !== "" ? root.problem : "Sysoscope"
    // What the panel opens from: the button, shifted by where the user dragged
    // the panel along the bar.
    Item {
      id: anchorProxy
      width: button.width
      height: button.height
      x: root.panel ? root.panel.anchorOffsetX : 0
      y: root.panel ? root.panel.anchorOffsetY : 0
    }
    onPressed: function(b) {
      if (b === Qt.MiddleButton) { if (root.service) root.service.runAction("refresh") }
      else if (b === Qt.RightButton) { if (root.bar) root.bar.run("omarchy-launch-or-focus-tui btop") }
      else root.openSection(root.lastSection)
    }
  }

  TextMetrics { id: gap; font: spacer.font; text: "  " }
  Text { id: spacer; visible: false; textFormat: Text.PlainText; font.family: button.fontFamily; font.pixelSize: button.fontSize }

  Row {
    id: segRow
    anchors.centerIn: parent
    visible: !root.vertical
    spacing: gap.advanceWidth
    opacity: button.opacity

    Repeater {
      model: root.segments
      BarSegment {
        required property var modelData
        segment: modelData
        look: root.look
        fontFamily: button.fontFamily
        fontSize: button.fontSize
        foreground: button.foreground
        urgentColor: root.bar ? root.bar.urgent : Color.urgent
        accent: root.bar && root.bar.accent !== undefined ? root.bar.accent : Color.accent
        barHeight: root.height
        readonly property bool tooltipHovered: segMouse.containsMouse

        MouseArea {
          id: segMouse
          anchors.fill: parent
          acceptedButtons: Qt.LeftButton
          hoverEnabled: true
          cursorShape: Qt.PointingHandCursor
          onEntered: if (root.bar) root.bar.showTooltip(parent, parent.modelData.tooltip || "Sysoscope")
          onExited: if (root.bar) root.bar.hideTooltip(parent)
          onClicked: root.openSection(parent.modelData.key === "icon" ? root.lastSection : parent.modelData.section)
        }
      }
    }
  }

  // Vertical bar: one line per metric, icon over value, like Argus.
  Column {
    id: column
    anchors.centerIn: parent
    visible: root.vertical
    spacing: Style.space(4)
    opacity: button.opacity

    Repeater {
      model: root.segments
      Text {
        required property var modelData
        width: root.width
        horizontalAlignment: Text.AlignHCenter
        text: modelData.text.replace(" ", "\n")
        textFormat: Text.PlainText
        color: modelData.urgent ? (root.bar ? root.bar.urgent : Color.urgent) : button.foreground
        font.family: button.fontFamily
        font.pixelSize: button.fontSize * 0.85
        readonly property bool tooltipHovered: segMouseV.containsMouse

        MouseArea {
          id: segMouseV
          anchors.fill: parent
          acceptedButtons: Qt.LeftButton
          hoverEnabled: true
          onEntered: if (root.bar) root.bar.showTooltip(parent, parent.modelData.tooltip || "Sysoscope")
          onExited: if (root.bar) root.bar.hideTooltip(parent)
          onClicked: root.openSection(parent.modelData.key === "icon" ? root.lastSection : parent.modelData.section)
        }
      }
    }
  }
}
