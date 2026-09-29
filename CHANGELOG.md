# Changelog

## 0.2.0 — 2026-09-29

- **Privacy on disk**: nothing that identifies the computer, its owner or
  what they do is written to disk; the panel still shows everything live.
  `latest.json` leaves out window titles, the inhibitors' program and
  reason and the class and title of the windows that keep the screen on,
  disk and monitor serial numbers, MAC addresses (also in Bluetooth
  battery names, and `enx`/`wlx`/`wwx` interface names made from them), the hostname, user names (paths under
  your home show `~`), snapper's allowed users, Wi-Fi network names, BSSIDs
  and connection ids, Bluetooth names and addresses, PipeWire device names,
  disk UUIDs, public IPv4 and non-loopback IPv6 addresses (also in a
  port's allowed sources), and the journal
  errors, crashes and OOM kills. The inventory cache (`static.json`) no
  longer keeps the hostname or the monitors' serial numbers (they are read
  again at start; an older cache is rewritten). `actions.log` no longer
  keeps the network, Bluetooth or audio device of an action, nor the text
  of network, Bluetooth, audio and window actions: only which action, when
  and whether it worked. Because of this, `snapshot` reuses `latest.json`
  only for cpu, memory, gpu, sensors and self, and `view` only for
  Summary, CPU and Memory; `search` always takes a snapshot.
- **Disk per app** on Applications: how much each app reads from and writes
  to the disk per second (from `/proc/<pid>/io`, only what reaches the
  disk), in each app's line and as a column of the apps without a window.
  Read only while Applications is open.
- **Power**: what keeps the computer or the screen awake (logind
  inhibitors, windows that keep the screen on, Omarchy's stay awake), the
  suspend mode and the devices that can wake the computer.
- **Devices**: each monitor's mode in use from Hyprland against the best
  refresh rate it offers at that resolution (with a note when HDMI offers
  less than the range in its EDID), and each USB device's version. The Summary warns about a monitor
  below a refresh rate it offers and a USB 3 device at USB 2 speed.
- **Security**: what listens on the network (each port with its program or
  system user and how far it reaches), whether the firewall is on (ufw,
  firewalld, nftables or iptables) and what it lets in for each port open to every
  network, ufw's rules, and which disks and swaps are encrypted. The
  Summary warns when nothing filters a TCP service that listens on every
  network, when ufw is enabled but not running, or when a swap is not
  encrypted while the system disk is. Read from the kernel (netlink
  `sock_diag` for TCP, `/proc/net` for UDP), ufw's files and sysfs, without
  root. New `snapshot --section security`.
- **Health** in Maintenance: this boot's errors in the journal (count, top
  sources, the latest five), the programs that crashed in the last 7 days
  (`coredumpctl`) and the processes killed for lack of memory (kernel and
  `systemd-oomd`); "Errors this boot" in the header. Read only while the
  section is open, every 30 seconds. Memory shows how many processes were
  killed since boot, and the Summary warns when the kernel killed any.
- **Maintenance** section (after About): whether a restart is needed after
  an update (kernel, Omarchy's flag, Hyprland, NVIDIA driver), the last
  full update, the disk space used by caches with the command to free each
  one, and snapper's snapshots or how to let your user see them. `u` checks
  the official repositories for updates with `checkupdates` (never the
  AUR). The Summary warns about a pending restart, more than 30 days without
  a full update and available updates. New `action updates.check`,
  `snapshot --section maintenance`.
- **Content search**: `/` still filters the sections by title and now also
  lists matches in the content of every section ("In the content"): the
  arrows go through both and Enter opens the result's section and picks
  its row. New stream command `search "QUERY"` and CLI `sysoscope search`.
- **Audio in Devices**: the outputs and inputs (like the bar, without
  unusable outputs), with their volume, mute and default. `d` makes one the
  default and moves what is playing, `m` mutes, `+`/`-` change the volume
  by 5 and `v` sets it. Read with `pactl` only while Devices is open.
- **Bluetooth in Devices**: whether it is on, and paired and nearby
  devices. `b` turns it on or off, `e` opens Omarchy's Bluetooth panel to
  pair, and `c`/`x`/`f` connect, disconnect and forget a paired device; the
  real outcome goes to `actions.log` (new internal command `sysoscope
  bt-wait`). BlueZ is never started by Sysoscope. Also in the lazylazy tab.
- **Power profile** in Power: `p` changes it with `omarchy-powerprofiles-set`
  (Omarchy remembers it for plugged in or on battery); on a laptop, `P`
  saves the profile for the other power source, and a row shows both. Also
  in the lazylazy tab.
- **Date and time** in About: time zone, automatic sync and whether the clock
  is synchronized (from `/etc/localtime`, the kernel clock and the
  `systemd-timesyncd` unit; not from `systemd-timedated`, which D-Bus starts
  on demand and would be restarted every minute). The Summary warns when the
  time is not set automatically or the clock is not synchronized.
- **Launcher entries**: "Sysoscope" (the panel) and "Sysoscope (terminal)"
  (the lazylazy tab, with `lazylazy sysoscope`) in SUPER+SPACE, removed when
  the plugin is disabled or removed. New IPC `ping`.
- **Wi-Fi in Network**: the nearby networks of each card (signal, band,
  security, saved or connected), and actions to connect, disconnect, forget,
  turn the Wi-Fi on or off, search again and pin the band. New networks with
  a password are joined in `nmtui`: no password goes through Sysoscope. Also
  in the lazylazy tab.
- **Hidden Wi-Fi networks** in Network: listed last, one per access point,
  as "(hidden network)". `c` asks for the network's name in a terminal and
  joins it with `nmcli`, which asks for the password there; neither the name
  nor the password is stored by Sysoscope or written to `actions.log`. New
  command `sysoscope wifi-hidden`.
- New **Applications** section: every app with its Hyprland windows and what
  it uses (CPU, memory, GPU, TCP network), with 20 actions on a window or the
  whole app. Also in the lazylazy tab.
- CPU, Memory, GPU and Network end with the three apps that use the most.
- The bar shows the apps you froze (❄) until they resume, after 5 minutes.

## 0.1.0

First release.

- Bar widget: pick which of 12 readings to show (CPU, memory, GPU, VRAM,
  temperatures, disk, network, load, battery, power profile), each as an
  icon, a value, a sparkline or a gauge, with thresholds that turn it red. Hovering shows a tooltip; clicking opens the panel
  on the last section you used.
- Panel with the Summary, the components (CPU, Memory, GPU, Disks, Network,
  Sensors, Power) and the system (About, Devices, Security). Keyboard first:
  `j`/`k`, `1`–`9`, initials, `/` to filter, `r` to refresh, Esc.
- Charts over 2 minutes, 1 hour, 24 hours or 7 days, from a history the
  collector keeps on disk.
- Settings inside the panel: what the bar shows and how, thresholds, the
  Summary cards, °C or °F, the sampling interval and the language (English
  and Spanish).
- Every value that cannot be read says why (missing tool, no permission,
  not present on this machine) instead of showing a blank.
- A look that follows the theme's colours: headline numbers, cards, history
  columns and section icons; the bar can be detailed or minimal (Settings →
  Bar look).
- The panel can be resized from its corner and moved by its title;
  Settings → Panel size resets it.
- Section headers say more: zram use in Memory, every awake GPU in GPU, the
  GPU's draw in Power on desktops without RAPL; a summary card that cannot be
  read keeps its title.
- A **System** tab in lazylazy (`lazylazy.toml`, `sysoscope lazylazy`).
- Measured cost: about 3.6 ms of CPU per sample and 13 MB of memory.
