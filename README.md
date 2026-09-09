# nanoclaude

Shows Claude Code usage on Nanoleaf panels.

The five-hour billing window fills the panels from the bottom up like a
liquid level, coloured green through red as it fills. While Claude is working,
a pulse of light travels along the arrangement. A Home Assistant toggle arms
and disarms the display, handing the panels back as ordinary lights when it is
off.

## Why it renders geometrically

The panels are not treated as N cells lit up in order. Nanoleaf reports each
panel's physical position, and a scene is a **field evaluated at those
positions** — so the picture follows however the panels are actually mounted
on the wall:

- The **fill level** is a function of height, so it genuinely rises through
  the shape rather than switching panels on in device order.
- The **activity pulse** travels along the shape's long axis, found by
  principal component analysis over the panel centroids. On a diagonal
  arrangement it runs along the diagonal; nothing is hardcoded.

`nanoclaude layout` prints the detected shape so you can confirm it matches
the wall.

## Install

```sh
just install                      # builds and installs to ~/.local/bin

# Pair with the panels: hold the controller's power button 5-7s until the
# LEDs flash. Panels hold several tokens, so this does not revoke Home
# Assistant's access.
nanoclaude pair --host 192.168.1.50

mkdir -p ~/.config/nanoclaude
cp deploy/env.example ~/.config/nanoclaude/env
chmod 600 ~/.config/nanoclaude/env
$EDITOR ~/.config/nanoclaude/env   # paste the token

# Confirm the shape is the right way up: green must be at the bottom.
nanoclaude calibrate

# Let Claude Code start it.
python3 deploy/install-hooks.py --dry-run   # inspect first
python3 deploy/install-hooks.py             # backs up before writing
```

There is no service to install and nothing to add to a startup file. The
hooks do two jobs: they report what Claude is doing, and they bring the
display up. `SessionStart` starts it, every other hook revives it, and the
daemon stands down by itself after 20 minutes with no live session — so it
exists exactly while Claude Code does.

Because it is started by a hook, the daemon reads its own settings from
`~/.config/nanoclaude/env` rather than relying on the launcher's environment.

### Running it another way

Nothing about the daemon requires the hooks. `deploy/nanoclaude.service` runs
it under systemd instead, which adds restart-on-failure and a sandbox
(`ProtectSystem=strict`, `ProtectHome=read-only`, `MemoryMax`); set
`NANOCLAUDE_IDLE_EXIT=0` so it stays up. A line in a compositor's startup
file works too. Without the hooks the display still shows the budget
gradient, just never the activity pulse.

## Commands

| Command | What it does |
|---|---|
| `nanoclaude up` | Start it in the background, unless already running. |
| `nanoclaude down` | Stop it and hand the panels back. |
| `nanoclaude status` | Whether it is running, and where its config and log live. |
| `nanoclaude run` | The daemon in the foreground. Restores the panels on exit. |
| `nanoclaude calibrate` | Light the panels to confirm which way the shape is mounted. |
| `nanoclaude pair --host <ip>` | Gets an API token from panels in pairing mode. |
| `nanoclaude layout` | Prints the panel arrangement and its scene coordinates. |
| `nanoclaude preview` | Draws a scene in the terminal, without touching the panels. |
| `nanoclaude hook` | Records a hook event. Called from `settings.json`, not by hand. |

`preview` works without a device, using a stand-in shape — useful for tuning
the look:

```sh
nanoclaude preview --budget 0.85 --phase tool --animate 8s
```

## Configuration

All via the environment; see `deploy/env.example`. Secrets are read from the
environment rather than flags so they never appear in `ps` output.

| Variable | Meaning |
|---|---|
| `NANOCLAUDE_NANOLEAF_HOST` | Panel controller address. Required. |
| `NANOCLAUDE_NANOLEAF_TOKEN` | Token from `nanoclaude pair`. Required. |
| `HASS_SERVER`, `HASS_TOKEN` | Home Assistant, to gate the display. Both or neither. |
| `NANOCLAUDE_TOGGLE_ENTITY` | Toggle entity. Default `input_boolean.claude_display`. |
| `NANOCLAUDE_LIMITS` | `cache` (default), `api`, or `off`. See below. |
| `NANOCLAUDE_LIMITS_CACHE` | Status line cache to read in `cache` mode. |
| `NANOCLAUDE_LIMITS_EVERY` | Poll interval in `api` mode. Floor 5m, default 15m. |
| `NANOCLAUDE_CEILING_COST` | What a full session costs, in dollars. Unset works it out. |
| `NANOCLAUDE_ROTATION` | Extra rotation in degrees. See `nanoclaude calibrate`. |
| `NANOCLAUDE_IDLE_EXIT` | Stand down after this long with no session. `0` never. Default 20m. |
| `NANOCLAUDE_FPS` | Frame rate, 1-60. Default 20. |

### Where the number comes from

Two sources, because neither is sufficient alone.

**The real one.** Claude Code's `/usage` screen reads `/api/oauth/usage`.
That gives the actual session utilization and reset time — the number that
matters.

By default this does **not** query it. A status line is already polling it
every 60 seconds: [claude-pulse](https://github.com/NoobyGains/claude-pulse)
leaves the answer in `~/.cache/claude-status/cache.json`, and reading that
file costs nothing, needs no credentials, and cannot get the account rate
limited. That last point is not hypothetical — probing the endpoint directly
while pulse was also polling it earned a 26-minute `Retry-After` during
development.

`NANOCLAUDE_LIMITS=api` queries it directly instead, for a machine with no
status line to borrow from. There the endpoint sets the pace: at most every 15
minutes, its `Retry-After` always wins, and any other failure backs off
geometrically.

**The local one.** `ccusage` reads the transcripts in `~/.claude/projects/`
and reports what the current five-hour block cost. That can be read
constantly but has no idea what the account's allowance is.

So a real reading becomes an *anchor*, and local cost carries the level
forward between anchors. The anchor also calibrates the fallback: it reveals
what a full session actually costs on this account — including a temporary
limit boost, which history could never account for — so if the endpoint stops
working, the estimate that takes over is one the real data taught it.

Note that **cost**, not token count, is the local proxy. `ccusage`'s
`totalTokens` weights every token type equally, and about 98% of it is cache
reads — the cheapest thing there is. That number mostly measures how long the
conversation has got, not how much allowance is gone.

## Development

```sh
just            # list recipes
just ci         # lint, test, vulnerability scan
just layout     # what shape does it think the panels are in
just preview    # animate a scene in the terminal
just logs       # follow the daemon
```

`just logs` follows the systemd journal. Started by a hook, the log is a file:
`nanoclaude status` prints the path.

Requires `ccusage` on `PATH`.
