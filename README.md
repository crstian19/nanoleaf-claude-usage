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
go install github.com/crstian19/nanoleaf-claude-usage/cmd/nanoclaude@latest
nanoclaude setup
```

`setup` does the whole job. It finds the panels on your network and pairs with
them. Then it writes the configuration file, checks that the shape is the
right way up, and installs the Claude Code hooks that start the display.

Every step is also a command of its own, so you can do any of them by hand:

```sh
nanoclaude discover                  # find the panels
nanoclaude pair --host 192.168.1.50  # get a token
nanoclaude calibrate                 # check which way the shape is mounted
nanoclaude hooks install             # let Claude Code start the display
```

`discover` scans your own networks for the Nanoleaf API port. It does not use
mDNS, because reaching an mDNS advert needs a resolver running on the machine,
and that is not a safe assumption. A sweep of a /24 takes under a second.

To pair, hold the power button on the controller for 5 to 7 seconds, until the
LEDs flash. The panels hold several tokens at once, so this does not revoke
access for Home Assistant or for the Nanoleaf app.

`calibrate` lights the bottom of the shape green and the top red, and lets you
turn it with the arrow keys until it matches your wall. Press enter and it
saves the rotation. The panels report where they are, and the device reports
how the arrangement is rotated, but nothing tells it which way is up in your
room.

You never have to name an angle. That was the earlier advice and it was not
something a person can do: working out the difference took a photograph and a
statistical fit of the panel positions.

`hooks install` writes to `~/.claude/settings.json`. It keeps every hook that
is already there and it writes a backup first. It also edits the file in
place, so keys it does not touch keep their order. Run it again after an
upgrade and it changes nothing.

## Usage

There is no service to enable and nothing to add to a startup file. Claude
Code starts the display. The `SessionStart` hook starts it. Every other hook
brings it back if it stopped. It shuts itself down after 20 minutes with no
live session.

You can also control it by hand:

| Command | What it does |
|---|---|
| `nanoclaude setup` | Set everything up, start to finish |
| `nanoclaude up` | Start it in the background, if it does not run already |
| `nanoclaude down` | Stop it and give the panels back |
| `nanoclaude status` | Report whether it runs, and where the log is |
| `nanoclaude run` | Run it in the foreground |
| `nanoclaude discover` | Find Nanoleaf controllers on this network |
| `nanoclaude pair` | Get an API token from panels in pairing mode |
| `nanoclaude calibrate` | Turn the shape until it matches your wall |
| `nanoclaude layout` | Print the panel positions and the scene coordinates |
| `nanoclaude preview` | Draw a scene in the terminal, without touching the panels |
| `nanoclaude hooks` | Install, remove, or report the Claude Code hooks |
| `nanoclaude brightness` | Report or set the panels' global brightness |

`nanoclaude preview` runs without a device. It falls back to a stand-in shape
of nine panels, which is useful to adjust the look:

```sh
nanoclaude preview --budget 0.85 --phase tool --animate 8s
```

Every command that draws a live view also has a plain form. A progress bar
written to a pipe is a stream of escape codes. The program asks whether the
output is a terminal, and prints one line instead.

The daemon gives the panels back when it stops. It restores the effect and the
power state that it found.

It never changes the panels' global brightness. Every colour it sends is
scaled by that value, so taking it over would give a predictable canvas, and
that was tried and reverted: the value belongs to whoever set it, in the
Nanoleaf app or in a home automation, and a daemon that restarts with every
session would overwrite that choice several times an hour. Use
`nanoclaude brightness` to set it on purpose.

## Configuration

The daemon reads its own configuration file at `~/.config/nanoclaude/env`. It
does this because a Claude Code hook starts it, and that hook knows nothing
about panels or tokens. `nanoclaude setup` writes the file, and the variables
below are all it understands.

| Variable | What it sets |
|---|---|
| `NANOCLAUDE_NANOLEAF_HOST` | Address of the panel controller. Required. |
| `NANOCLAUDE_NANOLEAF_TOKEN` | Token from `nanoclaude pair`. Required. |
| `HASS_SERVER`, `HASS_TOKEN` | Home Assistant, to gate the display. Both or neither. |
| `NANOCLAUDE_TOGGLE_ENTITY` | The switch that arms the display. |
| `NANOCLAUDE_LIMITS` | Source of the usage figure: `cache`, `api`, or `off`. |
| `NANOCLAUDE_LIMITS_CACHE` | Path of the status line cache, for `cache` mode. |
| `NANOCLAUDE_CEILING_COST` | Cost of a full session, in dollars. |
| `NANOCLAUDE_ROTATION` | Extra rotation in degrees. `nanoclaude calibrate` writes it. |
| `NANOCLAUDE_SKIP_SHAPES` | Shape numbers to treat as blanks, separated by commas. |
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

### Panel models

The device reports a shape number for every panel, and some shapes have no
LEDs: the Shapes controller brick and the Rhythm module both appear in a
layout like any other panel. Lighting one puts a dead spot in the middle of
every frame, so they are left out.

Only the shapes this version is sure about are named. Nanoleaf keeps
releasing models, and an exhaustive list would be wrong within a year in the
worst way: a real panel treated as a blank goes dark, and a blank treated as
a panel does the same. So an unnamed shape is still rendered, and
`nanoclaude layout` prints its number with a note. If it turns out to have no
LEDs, put that number in `NANOCLAUDE_SKIP_SHAPES`.

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
