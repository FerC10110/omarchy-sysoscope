// Launcher entries (SUPER+SPACE): "Sysoscope" opens the panel and
// "Sysoscope (terminal)" opens lazylazy on Sysoscope's tab. Service.qml
// installs them when the plugin loads and removes them when it goes away; the
// shell scripts below do the file work and are tested by node against a
// temporary directory. Only a file carrying MARKER is ever written or deleted:
// an entry of your own at those paths, or a symlink, is left alone.

var MARKER = "X-Sysoscope-Managed=true"

// The two entries of the plugin: file name and contents.
function entries(pluginId) {
  var common = ["Type=Application", "Icon=utilities-system-monitor", "Terminal=false", "StartupNotify=false",
                "Categories=System;Monitor;", "Keywords=system;monitor;hardware;cpu;gpu;sensors;", MARKER]
  function desktop(lines) { return ["[Desktop Entry]"].concat(lines, common).join("\n") + "\n" }
  return [
    { file: pluginId + ".desktop", text: desktop([
      "Name=Sysoscope", "Name[es]=Sysoscope",
      "Comment=Your hardware and what it is doing right now",
      "Comment[es]=Tu hardware y lo que está haciendo ahora",
      "Exec=omarchy-shell " + pluginId + " toggle", "TryExec=omarchy-shell"]) },
    { file: pluginId + "-terminal.desktop", text: desktop([
      "Name=Sysoscope (terminal)", "Name[es]=Sysoscope (terminal)",
      "Comment=Sysoscope as text, in lazylazy",
      "Comment[es]=Sysoscope en texto, en lazylazy",
      "Exec=omarchy-launch-tui lazylazy sysoscope", "TryExec=lazylazy"]) }
  ]
}

// $1 the marker, then pairs of destination and contents. A symlink or a file
// without the marker is skipped; each entry is written to a fresh sibling and
// moved over the old one only when it changed. Failures go to stderr (the
// shell's log) and exit 1; the other entries still get their turn.
var INSTALL = [
  'marker=$1; shift',
  'status=0',
  'while [ $# -ge 2 ]; do',
  '  dest=$1; text=$2; shift 2',
  '  [ -L "$dest" ] && continue',
  '  if [ -e "$dest" ] && ! grep -qxF "$marker" "$dest" 2>/dev/null; then continue; fi',
  '  dir=${dest%/*}',
  '  if ! mkdir -p "$dir" 2>/dev/null; then echo "cannot create $dir" >&2; status=1; continue; fi',
  '  if ! tmp=$(mktemp "$dest.XXXXXX" 2>/dev/null); then echo "cannot write $dest" >&2; status=1; continue; fi',
  '  if ! printf "%s" "$text" >"$tmp"; then rm -f "$tmp"; echo "cannot write $dest" >&2; status=1; continue; fi',
  '  chmod 644 "$tmp"',
  '  if cmp -s "$tmp" "$dest"; then rm -f "$tmp"',
  '  elif ! mv -f "$tmp" "$dest"; then rm -f "$tmp"; echo "cannot write $dest" >&2; status=1',
  '  fi',
  'done',
  'exit $status'
].join("\n")

// $1 seconds to wait, $2 a command that succeeds while the plugin is loaded,
// $3 the marker, then the entries. A plugin reload destroys the service and
// makes a new one at once: if the plugin still answers after the wait, the
// entries stay. Otherwise ours are deleted; symlinks and files without the
// marker are not.
var REMOVE = [
  'sleep "$1"',
  'if sh -c "$2" >/dev/null 2>&1; then exit 0; fi',
  'marker=$3; shift 3',
  'for f in "$@"; do',
  '  [ -L "$f" ] && continue',
  '  if grep -qxF "$marker" "$f" 2>/dev/null; then rm -f "$f"; fi',
  'done',
  'exit 0'
].join("\n")

// Where launchers look for user entries.
function applicationsDir(xdgDataHome, home) {
  var base = xdgDataHome && String(xdgDataHome).charAt(0) === "/" ? String(xdgDataHome) : home + "/.local/share"
  return base + "/applications"
}

function installArgv(dir, pluginId) {
  var argv = ["sh", "-c", INSTALL, "sh", MARKER]
  entries(pluginId).forEach(function(e) { argv.push(dir + "/" + e.file, e.text) })
  return argv
}

function removeArgv(dir, pluginId, waitSeconds, alive) {
  var argv = ["sh", "-c", REMOVE, "sh", String(waitSeconds), alive, MARKER]
  entries(pluginId).forEach(function(e) { argv.push(dir + "/" + e.file) })
  return argv
}

// Succeeds only while the plugin's IPC answers (omarchy-shell exits 1 on an
// unknown target).
function aliveCommand(pluginId) { return "omarchy-shell " + pluginId + " ping" }

if (typeof module !== "undefined") {
  module.exports = { MARKER: MARKER, entries: entries, INSTALL: INSTALL, REMOVE: REMOVE, applicationsDir: applicationsDir,
                     installArgv: installArgv, removeArgv: removeArgv, aliveCommand: aliveCommand }
}
