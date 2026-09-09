<p align="center">
  <img src="assets/logo.png" alt="nanoleaf-claude-usage" width="200">
</p>

<h1 align="center">nanoleaf-claude-usage</h1>

[![Go Reference](https://pkg.go.dev/badge/github.com/crstian19/nanoleaf-claude-usage.svg)](https://pkg.go.dev/github.com/crstian19/nanoleaf-claude-usage)
[![Go Version](https://img.shields.io/github/go-mod/go-version/crstian19/nanoleaf-claude-usage)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Show how much of your Claude Code session you have used, on a wall of Nanoleaf
panels.

The panels work as a gauge. Each panel owns an equal share of the session
allowance. A panel lights up when you spend its share. Its color tells you where it
sits on the scale, from green at the bottom to coral at the top. You read the
display by counting the lit panels. While Claude works, the whole
shape turns into a rainbow that sweeps across it.

The display fits the shape you mounted your panels in. It does not assume a
row or a grid.

## Install

```sh
just install
```

This builds the binary and copies it to `~/.local/bin`.

Next, pair with the panels. Hold the power button on the controller for 5 to 7
seconds, until the LEDs flash. Then run:

```sh
nanoclaude pair --host 192.168.1.50
```

The panels hold several tokens at once, so this does not revoke access for
Home Assistant or for the Nanoleaf app.

Now write the configuration file:

```sh
mkdir -p ~/.config/nanoclaude
cp deploy/env.example ~/.config/nanoclaude/env
chmod 600 ~/.config/nanoclaude/env
$EDITOR ~/.config/nanoclaude/env
```

Make sure that the shape is the right way up. This lights the bottom of the
shape green and the top red:

```sh
nanoclaude calibrate
```

If green is not at the bottom, set `NANOCLAUDE_ROTATION` to the difference in
degrees and run it again.

Last, let Claude Code start the display for you:

```sh
python3 deploy/install-hooks.py --dry-run
python3 deploy/install-hooks.py
```

The script appends to `~/.claude/settings.json`. It keeps every hook that is
already there, and it writes a backup first.

## Usage

There is no service to enable and nothing to add to a startup file. Claude
Code starts the display. The `SessionStart` hook starts it. Every other hook brings it
back if it stopped. It shuts itself down after 20 minutes with no live
session.

You can also control it by hand:

| Command | What it does |
|---|---|
| `nanoclaude up` | Start it in the background, if it does not run already |
| `nanoclaude down` | Stop it and give the panels back |
| `nanoclaude status` | Report whether it runs, and where the log is |
| `nanoclaude run` | Run it in the foreground |
| `nanoclaude calibrate` | Light the panels to make sure that the shape is the right way up |
| `nanoclaude layout` | Print the panel positions and the scene coordinates |
| `nanoclaude preview` | Draw a scene in the terminal, without touching the panels |
| `nanoclaude pair` | Get an API token from panels in pairing mode |

`nanoclaude preview` runs without a device. It falls back to a stand-in shape
of nine panels, which is useful to adjust the look:

```sh
nanoclaude preview --budget 0.85 --phase tool --animate 8s
```

The daemon gives the panels back when it stops. It restores the effect, the
brightness, and the power state that it found.

## Configuration

The daemon reads its own configuration file at `~/.config/nanoclaude/env`. It
does this because a Claude Code hook starts it, and that hook knows nothing
about panels or tokens. See `deploy/env.example`.

| Variable | What it sets |
|---|---|
| `NANOCLAUDE_NANOLEAF_HOST` | Address of the panel controller. Required. |
| `NANOCLAUDE_NANOLEAF_TOKEN` | Token from `nanoclaude pair`. Required. |
| `HASS_SERVER`, `HASS_TOKEN` | Home Assistant, to gate the display. Both or neither. |
| `NANOCLAUDE_TOGGLE_ENTITY` | The switch that arms the display. |
| `NANOCLAUDE_LIMITS` | Source of the usage figure: `cache`, `api`, or `off`. |
| `NANOCLAUDE_LIMITS_CACHE` | Path of the status line cache, for `cache` mode. |
| `NANOCLAUDE_CEILING_COST` | Cost of a full session, in dollars. |
| `NANOCLAUDE_ROTATION` | Extra rotation in degrees. See `nanoclaude calibrate`. |
| `NANOCLAUDE_BRIGHTNESS` | Device brightness while the display owns it. |
| `NANOCLAUDE_IDLE_EXIT` | Time with no session before it shuts down. `0` never. |
| `NANOCLAUDE_FPS` | Frame rate, from 1 to 60. |

Secrets come from the file and from the environment, never from a flag, so
they stay out of the output of `ps`.

### Home Assistant

The display can wait for a switch in Home Assistant. Create an
`input_boolean` helper and name it in `NANOCLAUDE_TOGGLE_ENTITY`. The daemon
only reads that switch. It never calls a service, so a fault here cannot
change anything else in your house.

Use an `https` address for `HASS_SERVER`. A Home Assistant long-lived token
has full access to the instance and it never expires. The daemon sends it on
every poll, so plain `http` puts it on the network thousands of times a day.
The daemon prints a warning at startup if the address is plain `http`.

## How it works

### Where the number comes from

Claude Code knows the real figure. Its `/usage` screen reads an endpoint that
reports the percentage of the session you have spent, and the time the session
resets.

By default this program does not call that endpoint. A status line already
polls it every 60 seconds.
[claude-pulse](https://github.com/NoobyGains/claude-pulse) writes the answer to
`~/.cache/claude-status/cache.json`, and reading that file costs nothing. It
needs no credentials, and it cannot get the account rate limited. The endpoint
limits requests hard. Two requests in quick succession earned a 26 minute
`Retry-After` during development.

Set `NANOCLAUDE_LIMITS=api` to call the endpoint directly, for a machine with
no status line to read from. Set `NANOCLAUDE_LIMITS=off` to keep every request
on your own network.

A real reading also works as an anchor. Between readings, the daemon adds the
cost that `ccusage` reports from the local transcripts. That cost is a
fallback, not the main source: the token count is about 98 percent cache
reads, which are the cheapest tokens there are, so it mostly measures the
length of the conversation. The anchor also teaches the daemon what a full
session costs on your plan, which a fallback cannot work out on its own.

### Why the display fits your shape

The device reports the position of every panel on the wall. The gauge fills
from the bottom of that shape, and the rainbow sweeps along the shape's long
axis. The program finds that axis with principal component analysis, a method
that finds the direction the positions vary in most. Nothing in the code
assumes a panel order or a panel count.

The device also reports a global orientation, because panel coordinates come
from the arrangement you built in the Nanoleaf app. That app does not know
which way is up on your wall. The program undoes that orientation. It does not
apply it again. On a real device at 302 degrees, the wrong sign put the
vertical axis 116 degrees out, and the gauge climbed diagonally.

### Colors

Both color scales are arcs in OKLCH, a color space where equal numbers look
equally bright.

The gauge holds lightness and chroma constant and moves only the hue. A scale
must be even, because a band that looked brighter would read as more urgent.
The cost is that the top of the scale is a warm coral and not a deep red. A
deep red has a low lightness, so it cannot appear on an arc of constant
lightness.

The rainbow holds lightness constant and takes as much chroma as each hue can
carry. A signal only has to be unmistakable, so any saturation left unused is
waste. One chroma for the whole hue circle has to fit the most limited hue,
which is about 0.13 here. The greens carry 0.48. The program measures the
gamut boundary for each hue at startup instead.

### Lifetime

A Claude Code hook starts the daemon, and the daemon holds a lock so that
three sessions do not start three daemons. The lock is a file lock, which the
kernel releases however the process dies. A stale process id file would block
every later start.

Every hook brings the daemon back, not only `SessionStart`. Without that, the
idle shutdown would be a trap. A session that stays open long enough for its
state file to expire never fires `SessionStart` again.

## Development

```sh
just            # list the recipes
just ci         # lint, race tests, vulnerability scan
just layout     # print the shape the program detected
just preview    # animate a scene in the terminal
just logs       # follow the daemon
```

Requires `ccusage` on `PATH`.

`deploy/nanoclaude.service` runs the daemon under systemd instead of under a
hook. Systemd adds a restart on failure and a sandbox. Set
`NANOCLAUDE_IDLE_EXIT=0` there, so the daemon stays up.

Read `docs/architecture.md` before you change the rendering or the wire
protocol. It records the faults behind the current design, and several of them
looked correct until a measurement showed otherwise.

## License

MIT. See [LICENSE](LICENSE).
