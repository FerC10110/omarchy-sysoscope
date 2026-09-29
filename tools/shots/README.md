# Offscreen screenshots

`shoot.sh` renders the real `Panel.qml` (or the bar) without a shell, with
stand-ins for `qs.Commons` and `qs.Ui`, to a PNG. It makes `docs/screenshots/`
and the Market preview.

    (cd collector && SYSOSCOPE_SHOTS=/tmp/shots go test ./internal/view -run TestWriteShots)
    tools/shots/shoot.sh /tmp/shots/threadripper-en dark cpu cpu.png

`shoot.sh` renders with OpenGL (`QSG_RHI_BACKEND=opengl`) because Qt's software
renderer draws no shader effects, and would leave out the glow on the big
numbers; it also sends Qt's messages to stderr (`QT_FORCE_STDERR_LOGGING=1`)
instead of the journal, so QML errors show up in the terminal.

## The Market preview and the README's GIF

```sh
tools/shots/shoot.sh /tmp/shots/threadripper-en dark preview preview.png   # 1920×1080
for s in summary cpu memory gpu storage network sensors about maintenance settings; do
  tools/shots/shoot.sh /tmp/shots/threadripper-en dark $s /tmp/shots/gif/$s.png; done
(cd /tmp/shots/gif && magick -delay 180 -loop 0 summary.png cpu.png memory.png gpu.png storage.png \
  network.png sensors.png about.png maintenance.png settings.png -resize 860x -colors 96 -layers Optimize tour.gif)
cp /tmp/shots/gif/tour.gif docs/tour.gif
```

`PreviewScene.qml` draws the bar across the top, the name and what the plugin
does on the left and the real panel on the Summary on the right, with what
matters in the middle (the Market's card crops the top and bottom).
