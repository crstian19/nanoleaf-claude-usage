<p align="center">
  <img src="assets/logo.png" alt="nanoleaf-claude-usage" width="200">
</p>

<h1 align="center">nanoleaf-claude-usage</h1>

[![CI](https://github.com/crstian19/nanoleaf-claude-usage/actions/workflows/ci.yml/badge.svg)](https://github.com/crstian19/nanoleaf-claude-usage/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/crstian19/nanoleaf-claude-usage)](https://github.com/crstian19/nanoleaf-claude-usage/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/crstian19/nanoleaf-claude-usage.svg)](https://pkg.go.dev/github.com/crstian19/nanoleaf-claude-usage)
[![Go Version](https://img.shields.io/github/go-mod/go-version/crstian19/nanoleaf-claude-usage)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Your Claude Code usage, on a wall of Nanoleaf panels.

Each panel owns an equal share of the session allowance and lights up when you
spend it, green at the bottom of the shape through to coral at the top. You
read the display by counting the lit panels. While Claude works, the whole
shape turns into a rainbow.

The display follows the shape you mounted.

## Install

Linux and macOS. Download a binary from
[releases](https://github.com/crstian19/nanoleaf-claude-usage/releases), or:

```sh
go install github.com/crstian19/nanoleaf-claude-usage/cmd/nanoclaude@latest
nanoclaude setup
```

`setup` finds the panels, pairs with them, writes the configuration, checks
the shape is the right way up, and installs the Claude Code hooks. Each step
is also its own command:

```sh
nanoclaude discover                  # find the panels
nanoclaude pair --host 192.168.1.50  # get a token
nanoclaude calibrate                 # line the shape up with your wall
nanoclaude hooks install             # let Claude Code start the display
```

To pair, hold the power button on the controller for 5 to 7 seconds, until
the LEDs flash. The panels hold several tokens at once, so this does not
revoke access for Home Assistant or for the Nanoleaf app.

`hooks install` edits `~/.claude/settings.json` in place. It keeps the hooks
already there, writes a backup first, and changes nothing when run twice.

## Calibrating

The device knows where its panels are relative to each other. It does not know
which way is up in your room.

`nanoclaude calibrate` opens a page on this machine that draws your own
panels, lights the bottom of the shape green and the top red, and follows your
mouse as you turn it. When it matches the wall, press Save.

The page is on the loopback interface, and its address works once, because the
address is handed to your browser where other programs can read it. Closing
the tab ends the session. `--tui` does the same job with the arrow keys.

The same page paints the real display at any level you choose, so you can see
what 90 percent looks like without spending it.

## Other shapes

```sh
nanoclaude preview --web                        # every sample, pick from the page
nanoclaude preview --shape hexagons-honeycomb   # one of them, in the terminal
```

Fifteen sample walls cover the range: Shapes triangles in rows, blocks and
zigzags, mini triangles, both sizes mixed, hexagons in honeycombs and columns,
Elements hexagons, Canvas squares, Aurora Light Panels, a Lines zigzag, the 4D
lightstrip round a screen, and a Skylight ceiling. Nothing reaches a device in
this mode, so it is safe to run while the display works.

The last entry is an empty wall. Drag a panel onto it and it sticks to
whichever edge you drop it on, with every place it could land drawn as you
drag. Panels go on in any order, an edge that is taken will not take another,
and `Undo`, `Clear` and a tick box for removing panels are there for the rest.

The palette offers every panel that goes together edge to edge. Panels only
clip to their own product line, and each is built at its published size, so a
Canvas square greys out on a wall of Shapes triangles. Lines and the lightstrip
join at connectors rather than edges, so each gets a sample instead.

## Usage

Claude Code starts the display. `SessionStart` brings it up, and every other
hook brings it back if it stopped. It shuts down after 20 minutes with no
session. There is nothing to enable and nothing to add to a startup file.

| Command | What it does |
|---|---|
| `nanoclaude setup` | Set everything up, start to finish |
| `nanoclaude up` | Start it in the background, if it does not run already |
| `nanoclaude down` | Stop it and give the panels back |
| `nanoclaude status` | Report whether it runs, and where the log is |
| `nanoclaude run` | Run it in the foreground |
| `nanoclaude discover` | Find Nanoleaf controllers on this network |
| `nanoclaude pair` | Get an API token from panels in pairing mode |
| `nanoclaude calibrate` | Turn the shape to match your wall |
| `nanoclaude layout` | Print the panel positions and the scene coordinates |
| `nanoclaude preview` | Draw a scene without touching the panels |
| `nanoclaude hooks` | Install, remove, or report the Claude Code hooks |
| `nanoclaude brightness` | Report or set the panels' global brightness |

`preview` runs without a device, on a sample shape, for tuning the look:

```sh
nanoclaude preview --budget 0.85 --phase tool --animate 8s
```

Every live view has a plain form for a pipe or a log.

When the daemon stops it restores the effect and the power state it found. It
never touches the panels' global brightness. That belongs to whoever set it,
in the Nanoleaf app or in a home automation. Use `nanoclaude brightness` to
change it on purpose.

## Configuration

The daemon reads `~/.config/nanoclaude/env`, because the Claude Code hook that
starts it knows nothing about panels or tokens. `nanoclaude setup` writes it.

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

Secrets come from the file or the environment, never from a flag, so they stay
out of `ps`.

### Home Assistant

The display can wait for a switch. Create an `input_boolean` helper and name it
in `NANOCLAUDE_TOGGLE_ENTITY`. The daemon only reads it and never calls a
service, so a fault here cannot change anything else in your house.

Use an `https` address for `HASS_SERVER`. A long-lived token has full access
and never expires, and the daemon sends it on every poll. Plain `http` is
warned about at startup.

## How it works

### The number

Claude Code's `/usage` screen reads an endpoint that reports the percentage of
the session spent and when it resets. That endpoint is rate limited hard, so
this program does not call it by default. A status line is already polling it.
[claude-pulse](https://github.com/NoobyGains/claude-pulse) writes the answer to
`~/.cache/claude-status/cache.json`, and reading a local file needs no
credentials and cannot get the account throttled.

`NANOCLAUDE_LIMITS=api` calls the endpoint directly, for a machine with no
status line. `NANOCLAUDE_LIMITS=off` keeps every request on your own network.

Between readings the daemon adds the cost `ccusage` reports from the local
transcripts. That figure is a fallback. The token count is about 98 percent
cache reads, so it mostly measures how long the conversation is. A real
reading also tells the daemon what a full session costs on your plan.

### The shape

The device reports where every panel sits. The gauge fills from the bottom of
that shape and the rainbow runs along its long axis, which comes from a
principal component analysis of the panel positions. Nothing in the code
depends on panel order or count.

The device also reports a global orientation, which comes from the arrangement
you built in the Nanoleaf app rather than from your wall. The program undoes
it, and `calibrate` covers the rest.

### Panels

All twenty shape numbers in Nanoleaf's documentation are named: Light Panels,
Canvas, Shapes, Elements, Lines, the 4D lightstrip and Skylight. Each is drawn
at its own published edge length, since a Shapes triangle is 134 and a hexagon
67, and Nanoleaf deprecated the single side length a device reports for that
reason.

Five pieces have no LEDs and are left out of every frame: the Rhythm module,
the Shapes controller, a Lines connector, a controller cap and a power
connector.

A shape number outside the list is still lit, and `nanoclaude layout` prints it
with a note. If it turns out to have no LEDs, put the number in
`NANOCLAUDE_SKIP_SHAPES`.

### Colors

Both scales are arcs in OKLCH, a color space where equal numbers look equally
bright.

The gauge moves only the hue, holding lightness and chroma fixed, so no band
reads as more urgent than its neighbour. The price is a warm coral at the top
instead of a deep red, which cannot exist at a constant lightness.

The rainbow keeps the lightness and takes all the chroma each hue can carry. A
single chroma for the whole circle would have to fit the most limited hue,
about 0.13 here, while the greens carry 0.48, so the gamut boundary is measured
per hue at startup.

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
hook, which adds a restart on failure and a sandbox. Set
`NANOCLAUDE_IDLE_EXIT=0` there.

`docs/architecture.md` has the reasoning: the wire protocol, the color work,
and the faults behind the current design. Read it before changing the
rendering.

### Releases

Tags are the versions. Pushing one builds Linux and macOS binaries for both
architectures and writes the release notes from the commit messages, which is
what the Conventional Commits hook is for:

```sh
git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```

## License

MIT. See [LICENSE](LICENSE).
