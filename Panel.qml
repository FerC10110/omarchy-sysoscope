import QtQuick
import QtQuick.Layouts
import qs.Commons
import qs.Ui
import "I18n.js" as I18n
import "PanelModel.js" as PanelModel
import "Theme.js" as Theme
import "blocks"

// Sysoscope panel: a sidebar of sections and the open section's view. The
// views come from the collector already built (blocks); this file only draws
// them and routes keys. It holds no data of its own: everything is read from
// Service.qml, which streams the focused view.
Panel {
  id: sysoscope   // not `root`: blocks reach the panel through `host`
  moduleName: "io.github.ferc10110.sysoscope"
  ipcTarget: "io.github.ferc10110.sysoscope"
  // The service owns the IPC target (one handler, not one per monitor).
  manageIpc: false

  property var anchorItem: null
  property var hostWidget: null
  property var service: null

  property string current: "summary"
  property var keyState: ({ index: 0, filtering: false, query: "" })

  // ---- the panel's size and place. Pixels, 0 = automatic. Plain properties,
  // not bindings: the drags assign to them, which would break a binding. They
  // are seeded from config.json and written back once, on release.
  property int cardWidth: 0
  property int cardHeight: 0
  property int panelX: 0
  property int panelY: 0
  // While a drag is in flight a config change must not snap the panel back.
  property bool adjustingPanel: false
  readonly property string barPos: bar && bar.position ? bar.position : "top"
  readonly property var placement: PanelModel.placement(panelX, panelY, barPos, Style.gapsOut)
  // BarWidget's anchor proxy reads these: the offset along the bar.
  readonly property int anchorOffsetX: placement.anchorX
  readonly property int anchorOffsetY: placement.anchorY

  function seedPanelGeometry() {
    if (adjustingPanel || !service) return
    var c = service.config
    cardWidth = c.panelWidth || 0
    cardHeight = c.panelWidth > 0 ? (c.panelHeight || 0) : 0
    panelX = c.panelX || 0
    panelY = c.panelY || 0
  }
  function setCardSize(w, h) {
    var s = PanelModel.clampSize(w, h, panel.availableCardWidth, panel.availableCardHeight, Style.space(100) / 100)
    cardWidth = s.width
    cardHeight = s.height
  }
  function setPanelOffset(x, y) {
    var o = PanelModel.clampOffset(x, y, barPos, panel.screenW, panel.screenH)
    panelX = o.x
    panelY = o.y
  }
  // One drag, one write.
  function savePanelGeometry() {
    adjustingPanel = false
    if (!service) return
    var c = service.config
    if (c.panelWidth === cardWidth && c.panelHeight === cardHeight && c.panelX === panelX && c.panelY === panelY) return
    service.saveConfig(Object.assign({}, c, { panelWidth: cardWidth, panelHeight: cardHeight, panelX: panelX, panelY: panelY }))
  }
  Connections {
    target: sysoscope.service
    function onConfigChanged() { sysoscope.seedPanelGeometry() }
  }
  onServiceChanged: seedPanelGeometry()

  // ---- the `host` API for blocks
  readonly property color foreground: bar ? bar.foreground : Color.foreground
  readonly property color background: Color.background
  readonly property color accent: (bar && bar.accent !== undefined) ? bar.accent : Color.accent
  readonly property color urgent: bar ? bar.urgent : Color.urgent
  readonly property color warn: Qt.tint(foreground, Qt.rgba(urgent.r, urgent.g, urgent.b, 0.65))
  function rgba(c) { return Qt.rgba(c.r, c.g, c.b, c.a) }
  readonly property color dim: rgba(Theme.dim(foreground))
  readonly property color card: rgba(Theme.card(background, foreground))
  readonly property color track: rgba(Theme.track(background, foreground))
  readonly property color accentHi: rgba(Theme.hi(accent))
  // Numbers, bars and columns: accent, or urgent when critical.
  function strongColor(level) { return level === "crit" ? urgent : accent }
  readonly property string fontFamily: bar ? bar.fontFamily : Style.font.family
  readonly property string language: service ? service.language : "en"
  function t(s, args) { return I18n.t(s, language, args) }

  // strong: meters and their numbers, which are accent even when fine.
  function levelColor(level, strong) {
    if (level === "crit") return urgent
    if (level === "warn") return warn
    if (level === "ok") return accent
    return strong ? accent : foreground
  }

  function follow(id) { select(id) }

  // ---- sections
  readonly property var entries: PanelModel.items(service ? service.sections : [], t)
  readonly property var shown: keyState.filtering ? PanelModel.filtered(entries, keyState.query) : entries

  // The content search: the collector's hits for the query, listed under the
  // filtered sections. searchCursor walks both (sections first).
  readonly property var hits: keyState.filtering
                              ? PanelModel.hitsFor(service ? service.searchResult : null, keyState.query) : []
  readonly property var searchItems: PanelModel.searchList(shown, hits)
  property int searchCursor: 0
  // A new reply can shrink or empty the hits: the cursor must not linger on
  // one that is gone.
  onHitsChanged: searchCursor = PanelModel.afterHitsChange(shown.length, searchCursor, hits.length)

  readonly property var view: service && service.view && service.view.id === current ? service.view : null
  readonly property var actions: view ? (view.actions || []) : []

  // The Applications section: the picked row, the question being asked in
  // the footer and the last result, shown for 4 s.
  property string selectedRow: ""
  property var lastRows: []
  // A row a search opened: picked once the view has rows, and scrolled to.
  property string pendingRow: ""
  property string revealRow: ""
  readonly property var rows: PanelModel.rowsOf(view)
  onRowsChanged: {
    var next = PanelModel.nextRow(lastRows, rows, selectedRow, pendingRow)
    selectedRow = next.selected
    pendingRow = next.pending
    // A row TableBlock will never draw (past its own 50-row cap): stop
    // waiting to scroll to it, or a later refresh could bring it within
    // reach and scroll there as a surprise.
    if (revealRow !== "" && selectedRow === revealRow && !PanelModel.reachable(rows, revealRow)) revealRow = ""
    lastRows = rows
    // The row a question was asked about is gone: the answer has no target.
    if (ask && askRow !== "" && PanelModel.rowIndex(rows, askRow) < 0) { ask = null; askError = "" }
  }
  readonly property var pickedRow: { var i = PanelModel.rowIndex(rows, selectedRow); return i < 0 ? null : rows[i] }
  readonly property var usable: PanelModel.actionsFor(actions, pickedRow)
  property var ask: null
  property string askError: ""
  // The row the question is about, fixed when it was asked: a later pick or
  // refresh must not send the action to another row.
  property string askRow: ""
  property string resultText: ""
  property bool resultFailed: false
  property bool resultShown: false

  Timer { id: resultTimer; interval: 4000; onTriggered: sysoscope.resultShown = false }

  // Ask the collector 150 ms after the last key.
  Timer {
    id: searchTimer
    interval: 150
    onTriggered: {
      if (sysoscope.keyState.filtering && sysoscope.service && sysoscope.service.searchContent)
        sysoscope.service.searchContent(sysoscope.keyState.query)
    }
  }
  Connections {
    target: sysoscope.service
    function onLastResultChanged() {
      var r = sysoscope.service.lastResult
      if (!r) return
      sysoscope.resultText = r.text
      sysoscope.resultFailed = !r.ok
      sysoscope.resultShown = true
      resultTimer.restart()
    }
  }

  function perform(a) {
    var s = PanelModel.askStart(a, a.target === "row" && pickedRow ? pickedRow.label : "")
    askRow = a.target === "row" ? selectedRow : ""
    if (s.run) runNow(s.run)
    else { ask = s.ask; askError = "" }
  }

  function runNow(r) {
    ask = null
    if (service) service.runAction(r.id, r.target === "row" ? askRow : "", r.input)
  }

  function answer(key) {
    var r = PanelModel.askKey(ask, key)
    if (r.cancel) { ask = null; return }
    if (r.run) { runNow(r.run); return }
    ask = r.ask
    askError = r.error || ""
  }
  readonly property string currentTitle: {
    var i = PanelModel.indexOf(entries, current)
    return i >= 0 ? entries[i].title : ""
  }

  function select(id) {
    if (!id) return
    ask = null; selectedRow = ""; lastRows = []; pendingRow = ""; revealRow = ""
    resultShown = false; resultTimer.stop()
    current = id
    if (service) service.rememberSection(id)
    keyState = { index: Math.max(0, PanelModel.indexOf(entries, id)), filtering: keyState.filtering, query: keyState.query }
    if (service && opened) service.setFocus(id === "settings" ? "" : id, sysoscope)
    content.contentY = 0
  }

  function openSection(id) {
    select(id || current)
    open()
  }

  onOpenedChanged: {
    if (opened) { if (service) service.setFocus(current === "settings" ? "" : current, sysoscope) }
    else {
      if (service) service.setFocus("", sysoscope)
      stopFilter()
      ask = null
    }
  }

  function move(dy) {
    var i = PanelModel.indexOf(shown, current)
    var j = Math.max(0, Math.min(shown.length - 1, (i < 0 ? 0 : i + dy)))
    if (shown.length > 0) select(shown[j].id)
  }

  function startFilter() {
    // The cursor starts on the current section (as move() finds it), not on
    // shown[0]: an empty query must not send Enter or Down away from it.
    var i = PanelModel.indexOf(shown, current)
    keyState = { index: keyState.index, filtering: true, query: "" }
    searchCursor = Math.max(0, i)
    if (service && service.clearSearch) service.clearSearch()
    filterInput.text = ""
    filterInput.forceActiveFocus()
  }

  function stopFilter() {
    searchTimer.stop()
    if (!keyState.filtering) return
    keyState = { index: keyState.index, filtering: false, query: "" }
    keyCatcher.forceActiveFocus()
  }

  // Up/Down while filtering: through the sections (opened on the spot, as
  // before), then through the hits (only marked).
  function searchStep(dy) {
    searchCursor = PanelModel.searchMove(searchItems, searchCursor, dy)
    var e = searchItems[searchCursor]
    if (!e) return
    if (e.kind === "section") select(e.id)
    else hitList.positionViewAtIndex(e.index, ListView.Contain)
  }

  // Enter or a click: a hit opens its section and picks its row; a section
  // is already open, so the filter just closes.
  function openSearch(i) {
    var o = PanelModel.searchOpen(searchItems, i, hits)
    stopFilter()
    if (!o) return
    if (!o.hit) { if (o.section !== current) select(o.section); return }
    select(o.section)
    if (o.row) {
      pendingRow = o.row
      revealRow = o.row
      var next = PanelModel.nextRow(rows, rows, selectedRow, pendingRow)
      selectedRow = next.selected
      pendingRow = next.pending
      // Same section, already loaded: reachability is known right away.
      if (selectedRow === revealRow && !PanelModel.reachable(rows, revealRow)) revealRow = ""
    }
  }

  // TableBlock calls this with the found row's first cell once it is drawn.
  // A cell the layout has not placed yet reads y <= 0 (a real row always
  // sits below the header): skipped without clearing revealRow, so
  // TableBlock's retry (onYChanged) gets another try once it is placed.
  function reveal(item) {
    if (!item) return
    var p = item.mapToItem(blocks, 0, 0)
    if (p.y <= 0) return
    content.contentY = Math.max(0, Math.min(Math.max(0, content.contentHeight - content.height), p.y - content.height / 3))
    revealRow = ""
  }

  readonly property var settingsView: current === "settings" ? settingsLoader.item : null

  function onText(text) {
    if (ask) { answer(text); return }
    if (settingsView && settingsView.handleKey(text)) return
    for (var i = 0; i < usable.length; i++)
      if (usable[i].key === text) { perform(usable[i]); return }
    // A key of an action this row cannot take does nothing (it must not jump
    // to a section that starts with that letter).
    for (var j = 0; j < actions.length; j++)
      if (actions[j].key === text) return
    var s = PanelModel.onKey(keyState, text, entries)
    if (s.filtering && !keyState.filtering) { startFilter(); return }
    if (s.action === "refresh") { if (service) service.runAction("refresh"); return }
    if (s.action === "close") { close(); return }
    if (s.index !== keyState.index && entries[s.index]) select(entries[s.index].id)
  }

  // Enter follows the first link of the view (the Summary's most pressing
  // warning); any link can be clicked.
  function firstLink(blocks) {
    for (var i = 0; i < (blocks || []).length; i++) {
      if (blocks[i].link) return blocks[i].link
      var inner = firstLink(blocks[i].blocks)
      if (inner) return inner
    }
    return ""
  }

  KeyboardPanel {
    id: panel
    anchorItem: sysoscope.anchorItem
    owner: sysoscope.hostWidget || sysoscope
    bar: sysoscope.bar
    open: sysoscope.opened
    focusTarget: keyCatcher
    // A size the user dragged, or the automatic one.
    contentWidth: panel.fittedContentWidth(sysoscope.cardWidth > 0 ? sysoscope.cardWidth : Style.space(860))
    contentHeight: sysoscope.cardHeight > 0
                   ? panel.cappedContentHeight(sysoscope.cardHeight)
                   : panel.fittedContentHeight(Style.space(620), Style.space(760))
    gap: sysoscope.placement.gap

    PanelKeyCatcher {
      id: keyCatcher
      anchors.fill: parent
      // While the filter field owns the keyboard, letters (j, k…) must reach it.
      blocked: sysoscope.keyState.filtering
      onCloseRequested: { if (sysoscope.ask) sysoscope.ask = null; else sysoscope.close() }
      onTabRequested: function(direction) { sysoscope.switchPanel(direction) }
      // x reaches us as "delete": it is the close-window key.
      onDeleteRequested: sysoscope.onText("x")
      // In Settings, j/k choose a row and h/l change it. In a view with rows
      // (Applications), j/k pick a row and h/l move through the sections;
      // elsewhere j/k move through the sections. While asking, h/l and the
      // arrows pick an option.
      onMoveRequested: function(dx, dy) {
        if (sysoscope.ask) { if (dx !== 0) sysoscope.answer(dx < 0 ? "Left" : "Right"); return }
        if (sysoscope.settingsView) {
          if (dy !== 0) sysoscope.settingsView.move(dy)
          else sysoscope.settingsView.handleKey(dx < 0 ? "h" : "l")
        } else if (sysoscope.rows.length > 0) {
          if (dy !== 0) sysoscope.selectedRow = PanelModel.moveRow(sysoscope.rows, sysoscope.selectedRow, dy)
          else sysoscope.move(dx)
        } else if (dy !== 0) sysoscope.move(dy)
      }
      onTextKey: function(text) { sysoscope.onText(text) }
      onActivateRequested: {
        if (sysoscope.ask) { sysoscope.answer("Return"); return }
        if (sysoscope.settingsView) { sysoscope.settingsView.handleKey(" "); return }
        for (var i = 0; i < sysoscope.usable.length; i++)
          if (sysoscope.usable[i].key === "enter") { sysoscope.perform(sysoscope.usable[i]); return }
        var link = sysoscope.view ? sysoscope.firstLink(sysoscope.view.blocks) : ""
        if (link) sysoscope.follow(link)
      }

      // Drag the title strip to move the panel. Declared before the layout so
      // it sits underneath: the sidebar rows, which take clicks, stay on top.
      // Scene coordinates, since the panel moves while it is dragged.
      MouseArea {
        id: mover
        width: keyCatcher.width
        height: titleRow.height + Style.space(4)
        preventStealing: true
        cursorShape: Qt.SizeAllCursor
        acceptedButtons: Qt.LeftButton
        property point dragStart
        property int startX: 0
        property int startY: 0
        onPressed: function(mouse) {
          dragStart = mapToItem(null, mouse.x, mouse.y)
          startX = sysoscope.panelX
          startY = sysoscope.panelY
          sysoscope.adjustingPanel = true
        }
        onPositionChanged: function(mouse) {
          if (!pressed) return
          var p = mapToItem(null, mouse.x, mouse.y)
          sysoscope.setPanelOffset(startX + p.x - dragStart.x, startY + p.y - dragStart.y)
        }
        onReleased: sysoscope.savePanelGeometry()
        // Losing the grab (the panel closing mid-drag) cancels instead.
        onCanceled: sysoscope.savePanelGeometry()
      }

      RowLayout {
        anchors.fill: parent
        spacing: Style.space(16)

        // ---- sidebar
        ColumnLayout {
          Layout.fillWidth: false
          Layout.minimumWidth: Style.space(200)
          Layout.maximumWidth: Style.space(200)
          Layout.fillHeight: true
          spacing: Style.space(2)

          TextInput {
            id: filterInput
            Layout.fillWidth: true
            visible: sysoscope.keyState.filtering
            color: sysoscope.foreground
            font.family: sysoscope.fontFamily
            font.pixelSize: Style.font.body
            onTextChanged: {
              if (!sysoscope.keyState.filtering) return
              var first = PanelModel.filtered(sysoscope.entries, text)[0]
              sysoscope.keyState = { index: sysoscope.keyState.index, filtering: true, query: text }
              sysoscope.searchCursor = 0
              if (first) sysoscope.select(first.id)
              if (PanelModel.searchWanted(text)) searchTimer.restart(); else searchTimer.stop()
            }
            Keys.onEscapePressed: sysoscope.stopFilter()
            Keys.onReturnPressed: sysoscope.openSearch(sysoscope.searchCursor)
            Keys.onEnterPressed: sysoscope.openSearch(sysoscope.searchCursor)
            Keys.onUpPressed: sysoscope.searchStep(-1)
            Keys.onDownPressed: sysoscope.searchStep(1)

            Text {
              anchors.right: parent.left
              anchors.rightMargin: Style.space(2)
              anchors.verticalCenter: parent.verticalCenter
              text: "/"
              textFormat: Text.PlainText
              color: sysoscope.dim
              font: parent.font
            }
          }

          Repeater {
            model: sysoscope.shown
            ColumnLayout {
              required property var modelData
              required property int index
              readonly property bool newGroup: index === 0 || sysoscope.shown[index - 1].group !== modelData.group
              readonly property bool selected: modelData.id === sysoscope.current
              Layout.fillWidth: true
              spacing: Style.space(2)

              Label {
                Layout.fillWidth: true
                Layout.topMargin: index === 0 ? 0 : Style.space(10)
                visible: parent.newGroup && text !== ""
                host: sysoscope
                text: PanelModel.groupTitle(parent.modelData.group, sysoscope.t)
              }

              Rectangle {
                Layout.fillWidth: true
                implicitHeight: row.implicitHeight + Style.space(8)
                radius: Style.space(6)
                color: parent.selected ? sysoscope.card : (itemMouse.containsMouse ? sysoscope.track : "transparent")
                border.width: 1
                border.color: parent.selected ? Qt.rgba(sysoscope.accent.r, sysoscope.accent.g, sysoscope.accent.b, 0.25) : "transparent"

                Rectangle {
                  anchors.left: parent.left
                  anchors.verticalCenter: parent.verticalCenter
                  anchors.leftMargin: Style.space(2)
                  width: Style.space(3)
                  height: parent.height - Style.space(10)
                  radius: Style.space(2)
                  visible: parent.parent.selected
                  color: sysoscope.accent
                }

                RowLayout {
                  id: row
                  anchors.fill: parent
                  anchors.leftMargin: Style.space(10)
                  anchors.rightMargin: Style.space(8)
                  spacing: Style.space(8)
                  Text {
                    Layout.preferredWidth: Style.space(16)
                    text: PanelModel.iconOf(modelData.id)
                    textFormat: Text.PlainText
                    color: selected ? sysoscope.accent : sysoscope.dim
                    font.family: sysoscope.fontFamily
                    font.pixelSize: Style.font.body
                  }
                  Text {
                    Layout.fillWidth: true
                    text: modelData.title
                    textFormat: Text.PlainText
                    elide: Text.ElideRight
                    color: selected ? sysoscope.accent : sysoscope.foreground
                    font.family: sysoscope.fontFamily
                    font.pixelSize: Style.font.body
                  }
                  Rectangle {
                    visible: (modelData.badge || 0) > 0
                    implicitWidth: Math.max(implicitHeight, badgeText.implicitWidth + Style.space(10))
                    implicitHeight: badgeText.implicitHeight + Style.space(2)
                    radius: height / 2
                    color: sysoscope.urgent
                    Text {
                      id: badgeText
                      anchors.centerIn: parent
                      text: String(modelData.badge || "")
                      textFormat: Text.PlainText
                      color: sysoscope.background
                      font.family: sysoscope.fontFamily
                      font.pixelSize: Style.font.caption
                      font.bold: true
                    }
                  }
                }
                MouseArea {
                  id: itemMouse
                  anchors.fill: parent
                  hoverEnabled: true
                  cursorShape: Qt.PointingHandCursor
                  onClicked: {
                    sysoscope.select(modelData.id)
                    if (sysoscope.keyState.filtering) sysoscope.searchCursor = index
                  }
                }
              }
            }
          }

          // The content hits, under their own heading; only while filtering.
          Label {
            Layout.fillWidth: true
            Layout.topMargin: Style.space(10)
            visible: sysoscope.hits.length > 0
            host: sysoscope
            text: sysoscope.t("In the content")
          }

          ListView {
            id: hitList
            Layout.fillWidth: true
            Layout.fillHeight: true
            visible: sysoscope.hits.length > 0
            clip: true
            spacing: Style.space(2)
            boundsBehavior: Flickable.StopAtBounds
            model: sysoscope.hits
            delegate: Rectangle {
              id: hitRow
              required property var modelData
              required property int index
              readonly property bool picked: sysoscope.searchCursor === sysoscope.shown.length + index
              width: hitList.width
              height: hitCol.implicitHeight + Style.space(8)
              radius: Style.space(6)
              color: picked ? sysoscope.card : "transparent"

              ColumnLayout {
                id: hitCol
                anchors.left: parent.left
                anchors.right: parent.right
                anchors.verticalCenter: parent.verticalCenter
                anchors.leftMargin: Style.space(8)
                anchors.rightMargin: Style.space(8)
                spacing: 0
                Text {
                  Layout.fillWidth: true
                  text: PanelModel.hitPlace(hitRow.modelData)
                  textFormat: Text.PlainText
                  elide: Text.ElideRight
                  color: sysoscope.dim
                  font.family: sysoscope.fontFamily
                  font.pixelSize: Style.font.caption
                }
                Text {
                  Layout.fillWidth: true
                  text: hitRow.modelData.text || ""
                  textFormat: Text.PlainText
                  elide: Text.ElideRight
                  color: hitRow.picked ? sysoscope.accent : sysoscope.foreground
                  font.family: sysoscope.fontFamily
                  font.pixelSize: Style.font.bodySmall
                }
              }
              MouseArea {
                anchors.fill: parent
                cursorShape: Qt.PointingHandCursor
                onClicked: sysoscope.openSearch(sysoscope.shown.length + hitRow.index)
              }
            }
          }

          Item { Layout.fillHeight: true; visible: !hitList.visible }
        }

        // Vertical divider between sidebar and content
        Rectangle {
          Layout.fillHeight: true
          width: 1
          color: sysoscope.track
        }

        // ---- content
        ColumnLayout {
          Layout.fillWidth: true
          Layout.fillHeight: true
          spacing: Style.space(8)

          RowLayout {
            id: titleRow
            Layout.fillWidth: true
            Layout.bottomMargin: Style.space(2)
            spacing: Style.space(10)

            Rectangle {
              width: Style.space(26)
              height: Style.space(26)
              radius: Style.space(6)
              color: Qt.rgba(sysoscope.accent.r, sysoscope.accent.g, sysoscope.accent.b, 0.12)
              border.width: 1
              border.color: Qt.rgba(sysoscope.accent.r, sysoscope.accent.g, sysoscope.accent.b, 0.25)

              Text {
                anchors.centerIn: parent
                text: PanelModel.iconOf(sysoscope.current)
                textFormat: Text.PlainText
                color: sysoscope.accent
                font.family: sysoscope.fontFamily
                font.pixelSize: Style.font.bodySmall
              }
            }

            Text {
              Layout.fillWidth: true
              text: sysoscope.currentTitle || "Sysoscope"
              textFormat: Text.PlainText
              elide: Text.ElideRight
              color: sysoscope.accent
              font.family: sysoscope.fontFamily
              font.pixelSize: Style.font.subtitle
              font.bold: true
              font.capitalization: Font.AllUppercase
              font.letterSpacing: Style.font.subtitle * 0.12
            }

            Rectangle {
              implicitWidth: omarchyPill.implicitWidth + Style.space(12)
              implicitHeight: omarchyPill.implicitHeight + Style.space(4)
              radius: Style.space(4)
              color: sysoscope.card
              border.width: 1
              border.color: sysoscope.track

              Text {
                id: omarchyPill
                anchors.centerIn: parent
                text: "OMARCHY"
                textFormat: Text.PlainText
                color: sysoscope.dim
                font.family: sysoscope.fontFamily
                font.pixelSize: Style.font.caption
                font.bold: true
                font.letterSpacing: 1.2
              }
            }
          }

          Rectangle {
            Layout.fillWidth: true
            height: 1
            color: sysoscope.track
            Layout.bottomMargin: Style.space(2)
          }

          TextBlock {
            Layout.fillWidth: true
            visible: sysoscope.service !== null && sysoscope.service.problem !== ""
            host: sysoscope
            block: ({ type: "text", level: "crit", text: sysoscope.service ? sysoscope.service.problem : "" })
          }

          Text {
            Layout.fillWidth: true
            visible: PanelModel.loading(sysoscope.current, sysoscope.view, sysoscope.service ? sysoscope.service.problem : "")
            text: sysoscope.t("Loading…")
            textFormat: Text.PlainText
            color: sysoscope.dim
            font.family: sysoscope.fontFamily
            font.pixelSize: Style.font.body
          }

          Flickable {
            id: content
            Layout.fillWidth: true
            Layout.fillHeight: true
            visible: sysoscope.current !== "settings"
            clip: true
            contentWidth: width
            contentHeight: blocks.implicitHeight
            boundsBehavior: Flickable.StopAtBounds

            Blocks {
              id: blocks
              width: content.width
              host: sysoscope
              blocks: sysoscope.view ? sysoscope.view.blocks : []
            }
          }

          Loader {
            id: settingsLoader
            Layout.fillWidth: true
            Layout.fillHeight: true
            active: sysoscope.current === "settings"
            visible: active
            source: "SettingsView.qml"
            onLoaded: item.host = sysoscope
          }

          // The footer: the question being asked, else the last result for
          // 4 s, else what the picked row can do.
          Text {
            Layout.fillWidth: true
            visible: text !== ""
            text: sysoscope.ask ? PanelModel.askText(sysoscope.ask, sysoscope.askError, sysoscope.t)
                  : sysoscope.resultShown ? sysoscope.resultText
                  : sysoscope.usable.map(function(a) { return "[" + a.key + "] " + a.label }).join("   ")
            textFormat: Text.PlainText
            wrapMode: Text.Wrap
            color: sysoscope.ask ? sysoscope.foreground : (sysoscope.resultShown && sysoscope.resultFailed) ? sysoscope.urgent : sysoscope.dim
            font.family: sysoscope.fontFamily
            font.pixelSize: Style.font.bodySmall
          }
        }
      }

      // Bottom-right grip: drag to resize. In the card's padding (negative
      // margins), so it never covers a button of the content.
      MouseArea {
        id: grip
        width: panel.padding + Style.space(4)
        height: panel.padding + Style.space(4)
        anchors.right: parent.right
        anchors.bottom: parent.bottom
        anchors.rightMargin: -(panel.padding + 2)
        anchors.bottomMargin: -(panel.padding + 2)
        z: 20
        hoverEnabled: true
        preventStealing: true
        cursorShape: Qt.SizeFDiagCursor
        acceptedButtons: Qt.LeftButton
        property point dragStart
        property int startWidth: 0
        property int startHeight: 0
        onPressed: function(mouse) {
          dragStart = mapToItem(null, mouse.x, mouse.y)
          // What is on screen: cardWidth/cardHeight are 0 until the first drag.
          startWidth = panel.contentWidth
          startHeight = panel.contentHeight
          sysoscope.adjustingPanel = true
        }
        onPositionChanged: function(mouse) {
          if (!pressed) return
          var p = mapToItem(null, mouse.x, mouse.y)
          sysoscope.setCardSize(startWidth + p.x - dragStart.x, startHeight + p.y - dragStart.y)
        }
        onReleased: sysoscope.savePanelGeometry()
        onCanceled: sysoscope.savePanelGeometry()

        Text {
          anchors.right: parent.right
          anchors.bottom: parent.bottom
          text: "◢"
          textFormat: Text.PlainText
          color: grip.containsMouse || grip.pressed ? sysoscope.accent : sysoscope.dim
          font.pixelSize: Style.font.caption
        }
      }
    }
  }
}
