# Working on Sysoscope

Sysoscope is an Omarchy shell plugin (QML) on top of a Go collector.

- `collector/` is the Go program. It reads the machine and builds the panel's
  views (`internal/view`), already translated. The QML only draws them.
- `Service.qml` runs `bin/sysoscope-linux-<arch> stream` once and keeps its
  latest line; `BarWidget.qml` (one per monitor) and `Panel.qml` only read it.
- The `.js` files next to the QML are plain functions with no QML, tested with
  `node --test tests/`. A `.js` that needs another one uses QML's
  `.import "X.js" as X`; `tests/load.js` resolves that line for node.
- Every text the user sees goes through `t("…")` (QML) or `i18n.T/F` (Go),
  and needs its Spanish entry: `I18n.js` and `collector/internal/i18n/es.go`.
  The coverage tests fail on a missing or unused translation.
- Every `Text` sets `textFormat: Text.PlainText` (`tests/plaintext.test.js`):
  device names, SSIDs and process names come from the machine.

## Before every release

1. `make test` (Go and node) and `make verify-bin`: both green.
2. Restart the shell (`omarchy restart shell`; the automatic reload on file
   changes keeps the old compiled QML) and read the log,
   `/run/user/$UID/quickshell/by-id/*/log.log`: no `sysoscope` errors.
3. Go through the 12 sections and Settings, in English and in Spanish
   (change the language in Settings). Check that the keys work (`j`/`k`,
   `1`–`9`, initials, `/`, `r`, Esc) and that the Summary's links open their
   section.
4. Check the bar with one monitor and with two: `omarchy-shell
   io.github.ferc10110.sysoscope open cpu` must open a single panel.
5. Rename `bin/sysoscope-linux-amd64`, reload: the bar dims and says the
   collector is missing. Restore it, reload: it comes back on its own. Kill
   the collector (`pkill -f "sysoscope-linux-amd64 stream"`): it restarts,
   and `pgrep -c -f "sysoscope-linux-amd64 stream"` stays at 1.
6. Measure: run `stream` for 60 s with the panel closed and with the panel
   open on CPU. The goal is under 5 ms of CPU per sample and under 15 MB of
   memory (Settings shows both).
7. Render the 13 screens with `tools/shots/shoot.sh` in the dark and light
   themes and look at every one.
8. Bump the version in `manifest.json` and `collector/VERSION` (the same
   number), run `make bin`, then `make verify-bin` twice.

Never commit a `bin/` that `make verify-bin` does not reproduce.
