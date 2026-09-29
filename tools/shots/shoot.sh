#!/usr/bin/env bash
# Renders the real panel (or bar) offscreen to a PNG.
# usage: shoot.sh DATA_DIR THEME SECTION OUT.png [LOOK]
#   DATA_DIR  a folder written by: SYSOSCOPE_SHOTS=DIR go test ./internal/view -run TestWriteShots
#   THEME     dark | light | blue (tools/shots/themes)
#   SECTION   summary, cpu, … settings, or bar
#   LOOK      minimal | detailed (bar only)
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
QT_FORCE_STDERR_LOGGING=1 QT_QUICK_BACKEND=rhi QSG_RHI_BACKEND=opengl QT_QPA_PLATFORM=offscreen QML_XHR_ALLOW_FILE_READ=1 \
  /usr/lib/qt6/bin/qml -I "$here" "$here/Main.qml" -- \
  "$(cd "$1" && pwd)" "$3" "$(realpath -m "$4")" "${5:-detailed}" "$here/themes/$2.json"
