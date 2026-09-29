# Binary provenance

Sysoscope ships two prebuilt programs, `bin/sysoscope-linux-amd64` and
`bin/sysoscope-linux-arm64`. They are the collector in `collector/`,
compiled. This file says how to check that claim yourself, and what the
program does once it runs.

## They are built from this source, byte for byte

The build is reproducible: the same source and the same Go version give the
same bytes on any machine.

- Go version: pinned in `collector/go.mod` (`toolchain` line). Go downloads
  that exact toolchain if yours differs.
- Flags (`Makefile`): `CGO_ENABLED=0 -trimpath -buildvcs=false
  -ldflags="-s -w -buildid= -X main.version=<collector/VERSION>"`. No C code,
  no paths from the build machine, no timestamps, no git data.
- Dependencies: pinned with checksums in `collector/go.sum`
  (`github.com/godbus/dbus/v5` and `golang.org/x/sys`, besides the standard
  library).

To check, with Go installed:

```sh
make verify-bin
```

It rebuilds both binaries into `collector/dist/`, compares them with `bin/`
byte by byte, and checks `bin/SHA256SUMS`. It fails loudly on any
difference. CI runs it on every push (`.github/workflows/ci.yml`), so a
commit whose binaries do not match its source shows a red check.

## Releases are attested

A tag `vX.Y.Z` runs `.github/workflows/release.yml` on GitHub's runners. It
runs the tests, runs `make verify-bin`, and only then signs a build
provenance attestation for the two binaries and attaches them to the
release. Every action is pinned by commit SHA. To check a copy you have:

```sh
gh attestation verify bin/sysoscope-linux-amd64 --repo FerC10110/omarchy-sysoscope
```

## What the program does

`Service.qml` starts one process, `sysoscope-linux-<arch> stream`, and reads
one JSON line per sample from it. The program:

- **Reads** `/proc`, `/sys` and `/run/udev` (CPU, memory, disks, network,
  sensors, battery, devices; while Applications is open, also
  `/proc/<pid>/io` of your own processes for the disk per app),
  `/etc/localtime` (the time zone), the kernel
  clock's sync status (`adjtimex(2)` without changing anything), and the
  D-Bus system bus: disk health (`org.freedesktop.UDisks2`), power profiles
  (`net.hadess.PowerProfiles`), failed units and the state of
  `systemd-timesyncd` (`org.freedesktop.systemd1`) and Bluetooth adapters and
  devices (`org.bluez`, only if it is already running: it asks
  `NameHasOwner` first, so D-Bus never starts it). Only property reads and
  queries; it never calls a method that changes anything, and it does not
  ask `org.freedesktop.timedate1` (D-Bus would start `systemd-timedated` for
  every question), and it also reads `/sys/class/rfkill` (the Bluetooth
  block).
  For Maintenance it reads `/var/log/pacman.log` (only when it changed),
  `/etc/pacman.conf`, `/usr/lib/modules/*/vmlinuz`,
  `/sys/module/nvidia/version`, the package folders in
  `/var/lib/pacman/local`, Omarchy's restart flags in
  `~/.local/state/omarchy`, Hyprland's lock file and `/proc/<pid>/exe`,
  snapper's configs and `/etc/group`; it walks the cache folders (pacman,
  yay, paru, crash dumps) to add up their size, without following links;
  and it asks snapperd (`org.opensuse.Snapper.ListSnapshots`) only if
  snapper's config lets your user see the snapshots.
  For the health on Maintenance it also reads `/etc/passwd` (the user
  names of crashed programs, and also for the owners of listening ports);
  `oom_kill` comes from `/proc/vmstat`.
  For Security it asks the kernel for the TCP sockets in LISTEN through
  netlink `sock_diag` (read-only, no root; the first request may make the
  kernel load its `inet_diag`/`tcp_diag` modules on its own, as `ss` does;
  `/proc/net/{tcp,tcp6}` only if that fails, and in `fixture capture`,
  which keeps only the listening lines), and reads `/proc/net/{udp,udp6}`,
  `/proc/sys/net/ipv4/ip_local_port_range`, ufw's files
  (`/etc/ufw/ufw.conf`, `/etc/ufw/user.rules`, `/etc/ufw/user6.rules`,
  `/etc/default/ufw`, only when they changed) and `/sys/block/dm-*`; with
  Security open, the open files of your own processes (`/proc/<pid>/fd`)
  to name the program behind each port. The state of `ufw.service`,
  `firewalld.service`, `nftables.service`, `iptables.service` and
  `ip6tables.service` comes in the same `ListUnitsByNames` call as
  `systemd-timesyncd`. It never runs `ufw`,
  `ss`, `iptables`, `nft`, `dmsetup` or `cryptsetup`.
  For Power it reads `/sys/power/mem_sleep`, the `power/wakeup` of USB and
  PCI devices and Omarchy's `~/.local/state/omarchy/indicators/stay-awake`,
  and, only while Power is open, asks logind for its inhibitors
  (`org.freedesktop.login1.Manager.ListInhibitors`, a query, after
  `NameHasOwner`) and Hyprland's request socket for `j/clients`. For
  Devices it reads each USB device's `version`, `bDeviceClass`, first
  interface's `bInterfaceClass` and `port/peer`, and asks Hyprland's socket
  for `j/monitors` (with the slow sources, and every 5 samples while Devices
  is open).
- **Runs**, each only if installed, with a timeout, `LC_ALL=C` and never
  through a shell: `lsblk` (disk layout), `pacman -Qqm` (package counts),
  `systemd-analyze time` (boot times), `nvidia-smi` (NVIDIA GPU readings),
  `inxi -m` (memory modules), `iw dev <interface> link` (Wi-Fi network name),
  `omarchy-powerprofiles-set <ac|battery> <profile>` (or, without it,
  `powerprofilesctl set <profile>`) only when you press `p` on Power,
  `pactl --format=json list sinks|sources` and `pactl --format=json info`
  (audio, only while Devices is open), `omarchy-audio-sink-availability`
  (the first read, when the outputs or their ports change, and after making
  a device the default; not on mute, volume or the other audio keys, even
  held down), and only when you press a key on Devices:
  `pactl set-sink-mute|set-source-mute|set-sink-volume|set-source-volume|set-default-sink|set-default-source`,
  `omarchy-audio-output-set-default` or `omarchy-audio-input-set-default`,
  `omarchy-shell omarchy.bluetooth open`, and, through a detached
  `sysoscope bt-wait`, `omarchy-bluetooth-power on|off` or
  `omarchy-bluetooth-device connect|disconnect|forget <address>`,
  `journalctl --disk-usage` (with Maintenance open, every 10 minutes),
  `journalctl -b -p err -a -o json -n 1000` (this boot's errors),
  `coredumpctl --json=short list --since=-7d` (crashes of the last 7 days),
  `journalctl -b -o json -g '^Killed ' SYSLOG_IDENTIFIER=systemd-oomd` and, only if the
  kernel killed something, `journalctl -b -k -o json -g 'of memory: Killed
  process'` (with Maintenance open every 30 seconds; the last two also
  with Memory open),
  `checkupdates --nocolor` and `pacman -Sddp --print-format '%r %n'
  --dbpath <checkupdates' copy> <packages>` (only when you press `u` on
  Maintenance; never `yay` or `paru`), and `fwupdmgr get-updates --json`
  (firmware updates, only when you press
  `f` on About).
- **Writes** only its own files: `~/.config/sysoscope/config.json` (your
  settings), the history under `~/.local/state/sysoscope/`, an inventory
  cache under `~/.cache/sysoscope/` and `~/.cache/sysoscope/updates.json`
  (the last update check), the last snapshot (`latest.json`) and a log of
  the actions you ran (`actions.log`) in `~/.local/state/sysoscope/`, none
  of them with anything that identifies the computer or you (serials,
  MACs, hostname, user names, network names, window titles: the README's
  Privacy section lists it); and, only when you press `P` on Power,
  Omarchy's saved profile for the other power source
  (`~/.local/state/omarchy/powerprofiles/ac` or `battery`). The plugin's QML
  (not this program) writes the two launcher entries in
  `~/.local/share/applications/`.
- **Network**: none of its own. The only request that leaves the machine is
  fwupd's, which fwupd makes to the LVFS when you ask it to check for
  firmware updates.
- **Privileges**: none. It runs as you, asks for no `sudo`, `pkexec` or
  capabilities, and shows "no permission" for anything your user cannot read.

`collector/fixtures/` holds recorded machines (text copies of `/proc`,
`/sys` and command output, with serials and addresses replaced) that the
tests run against. They are data; nothing in them is executed.
