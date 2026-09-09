# Architecture

## Data flow

```
status line cache (every 30s) ────┐  real session utilization  (anchor)
ccusage (subprocess, every 60s) ──┤  local cost                (interpolation)
                                  ├─→ render.Scene ─→ UDP frames (20/s) ─→ panels
Claude Code hooks (every 200ms) ──┤  activity phase
                                  │
Home Assistant toggle (every 5s) ─┘  arms / disarms
```

Three inputs on independent clocks, one render loop. Every input is polled on
its own goroutine and published to the loop over a buffered channel with a
non-blocking send: `ccusage` takes roughly 200 ms, which at 20 fps would stall
four frames and show up as a visible hitch in the travelling pulse. Dropping a
sample when the loop is behind costs nothing, because the next one is seconds
away.

## Why UDP extControl instead of Home Assistant

Home Assistant exposes the panels as a single `light` entity: one colour for
the whole array. That is enough for a lamp and useless for a display.

Nanoleaf's local API has a streaming mode (`extControl` v2) that accepts a
colour per panel over UDP on port 60222. Frames are big-endian:

```
uint16  panel count
per panel:
  uint16  panel id
  uint8   R, G, B, W
  uint16  transition time, in tenths of a second
```

UDP is fire-and-forget by design here: the device never acknowledges a frame,
so a lost packet means one skipped image out of twenty that second. That is
the right trade for a display, and it is why `Send` is cheap enough to call at
frame rate.

Two consequences worth knowing:

- The white channel is always sent as zero. Shapes panels have no dedicated
  white LED and setting it only washes the colour out.
- A transition time of zero makes the panels jump between frames, which reads
  as flicker. It is floored at one tick, which is also why the transition is
  set to the frame interval: each frame blends into the next instead of
  stepping.

Streaming mode stays active on the device until some other effect is selected,
so disarming has to explicitly hand control back — see below.

## Rendering is geometric, not index-based

This is the design decision everything else follows from.

`/panelLayout/layout` gives each panel a centroid on the wall, with Y
increasing upwards. `render.NewGeometry` turns those into scene coordinates:

- **U, V** — position within the arrangement's bounding box, V measured from
  the bottom. The budget fill is a function of V, so it rises through the
  shape.
- **S** — position along the arrangement's long axis, found by principal
  component analysis over the centroids. The activity pulse travels along S.

PCA matters because the long axis is a property of the mounting, not an
assumption. For panels in a diagonal band the axis is diagonal, and the pulse
runs through the figure the way it looks like it should. A hardcoded
horizontal or vertical sweep would cut across the shape instead.

The closed-form 2×2 eigenvector degenerates when the covariance's off-diagonal
term vanishes, which is exactly what a perfectly vertical column or horizontal
row produces — those cases are handled explicitly rather than left to
floating-point luck.

### The rainbow follows the gamut, the ramp does not

Both are OKLCH arcs at constant lightness, and they differ in one deliberate
way: the budget ramp holds chroma constant too, while the rainbow takes as
much as each hue will physically bear.

That is because they answer to different constraints. The ramp is a *scale*,
so it has to be even -- a band that happened to be more saturated would read
as more urgent. The rainbow is a *signal*, and the only thing asked of it is
to be unmistakable, so anything left on the table is waste.

And there is a lot on the table. A single chroma for the whole hue circle has
to be the smallest hue's maximum: at this lightness that is about 0.13, set by
a narrow stretch of blue-purple, while the greens and yellows would take 0.48.
Holding all 360 hues down to 0.13 threw away most of the available saturation
to accommodate one part of the circle. Following the boundary per hue makes
over 300 of the 360 hues more saturated, and turns the green from a pale
(118,196,121) into a pure (0,217,0).

The boundary is found by bisection once at startup, sampled at 256 hues and
interpolated, with a 4% margin that covers both the last bisection step and
the interpolation between samples. A test sweeps 3600 hues to confirm nothing
clips, because a clipped hue is not the colour it claims to be -- it would
appear as a flat patch at some angles of the sweep and not others.

This is also why the ambient highlight is suppressed while the rainbow turns.
Lifting a fully saturated hue leaves the gamut -- measured at 1.18 on the
worst channel -- and the rainbow is already motion, so the highlight was
redundant as well as harmful.

### A ramp of constant lightness

The budget ramp is an arc in OKLCH with lightness and chroma fixed, sweeping
hue from green to red. Only the hue changes.

The hand-picked sRGB ramp it replaces varied in measured lightness from 0.674
at the red end to 0.843 in the middle -- and was not even monotonic, so the
ambers were the brightest thing on the wall. Two things followed, both noticed
from across the room before being measured: the gradient did not read as an
even scale, and the ambient wave appeared to skip the brighter bands, because
removing a third of the light from something already glaring is far less
visible than removing it from something dimmer. With lightness fixed, the wave
moves 153-207 bytes on every band instead of 212-283 on some and far less in
perceived terms on others.

The price is that the top of the scale is a warm coral rather than a deep red:
a true deep red has a low lightness by definition, so it cannot appear on a
constant-lightness arc. Evenness was worth more than the last bit of menace.
Chroma sits at the most this hue arc will take without leaving the sRGB gamut,
which is checked by a test across the whole ramp -- a clipped colour is not
the colour it claims to be, and would reintroduce exactly the unevenness this
removes.

### Two colour spaces, on purpose

Colours are defined in sRGB and never blended in it: mixing sRGB numbers makes
a half-brightness green come out muddy and turns the fill's soft edge into a
visible step. But which space to blend in depends on what the blend *means*:

- **Interpolating between two colours is perceptual**, so it happens in
  **OKLab**. In linear light, green→red passes through a bright yellow-green
  that reads as a third colour rather than a midpoint — enough that the budget
  ramp originally needed an extra hand-placed stop just to hide it. OKLab
  interpolates evenly and that stop is gone; the ramp is three stops, not five.
- **Brightness and adding light sources are physical**, so `Scale` and `Add`
  stay in **linear light**. The activity pulse *adds* photons on top of the
  base colour, which is not a perceptual question — doing that sum in OKLab
  would be wrong.

The fill boundary is a `smoothstep` about one panel-height wide, so a panel
straddling the level sits at partial brightness — that is what makes it read
as a liquid level rather than a row of switches.

`clamp01` maps NaN to 0 rather than propagating it. Neither `math.Min`/`Max`
nor the `min`/`max` builtins reject NaN, and Go leaves an out-of-range
float-to-int conversion implementation-defined — so a NaN reaching the wire
would become an arbitrary byte. A degenerate layout can produce NaN
coordinates, and `Input` is exported, so this is the one place that stops it.

Unfilled panels keep a faint glow rather than going black, so the shape stays
readable as a shape in a dark room instead of looking half broken.

## Activity via hook state files

Claude Code hooks are short-lived processes. They cannot hold state, and
making each one talk to the daemon over a socket would put a connection on the
critical path of every tool call.

Instead `nanoclaude hook` writes one small file per session naming the latest
event, and a poller reads the directory — a poller, not a read inside the
frame case, for the same reason as the others: `os.ReadDir` plus a JSON decode
per live session, at frame rate, would put blocking filesystem I/O on the
render loop. The phase cannot change meaningfully faster than that anyway.

- **One file per session** so concurrent sessions — worktrees, subagents —
  never overwrite each other. The daemon takes the busiest phase across all of
  them, so an idle worktree cannot mask one that is working.
- **Written atomically** (temp file, then rename) so the daemon never reads a
  half-written record.
- **Session IDs are validated, not sanitised.** They arrive in a hook payload
  from outside the process and are used as filenames; anything path-shaped is
  refused outright.
- **Stale files are ignored and swept.** A session that was killed rather than
  closed leaves its last event behind forever, and without an age cut-off the
  display would show a phantom session indefinitely.

The hook command deliberately swallows its own errors and always prints `{}`.
A wall light must never be able to interrupt a coding session.

## Handing the panels back

The daemon saves the device's power state and selected effect before it
streams anything, and restores them when disarmed or shut down.

Brightness is deliberately not in that set, and this went the wrong way once.
Every colour sent over the wire is scaled by the device's global brightness,
so a device left at 50 halves a display whose contrast was calibrated byte by
byte, and taking the value over looked like the obvious fix. It is not. That
value belongs to whoever set it, in the Nanoleaf app or in a home automation,
and the daemon stands down when idle and restarts on the next hook: in one
afternoon it had forced the brightness to full 22 times. The `brightness`
command sets it when that is actually what someone wants.

It re-reads that state at each take-over, so a change made by hand while
disarmed is preserved rather than clobbered — but only if the saved effect is
restorable. Effect names beginning with `*` are the device's own
pseudo-effects, `extControl` among them, and letting one of those overwrite
the last known good effect is how a user's effect gets lost permanently.
When there is nothing meaningful to restore, the panels are powered off
instead: leaving them streaming a frozen frame would look broken.

### Enabling the stream is the only destructive step

`OpenStream` dials its UDP socket *before* it enables extControl, which looks
like the wrong order and is the whole point. The `PUT` that enables streaming
is the only step that changes the device, and it cannot be undone without
knowing which effect was selected beforehand. Were the local dial to fail
after that `PUT`, the panels would be stranded in streaming mode and — since
the device then reports its effect as `*ExtControl*` — the user's real effect
would be unrecoverable. A UDP dial makes no round trip, so doing it first
makes the function effectively atomic.

### The model is checked at startup

`NL22` (the original Light Panels/Aurora) is triangular like the Shapes
Triangles and easy to mistake for them, but it only speaks extControl **v1**
on a different port. Because UDP never answers, driving one with v2 frames
fails completely silently — dark panels, no error anywhere. So the daemon
logs the model and firmware at startup and says so loudly if the model cannot
accept what it sends.

Restore runs on a fresh context, because during shutdown the daemon's own
context is already cancelled and handing the panels back is precisely the work
that still has to happen.

## Where the number comes from

This went through two wrong answers before the right one, and both are worth
recording because they look reasonable.

**Wrong answer one: count tokens.** `ccusage`'s `totalTokens` sums input,
output, cache creation and cache reads with equal weight. Measured on a real
session, **98% of it was cache reads** — the cheapest token type there is,
around a tenth the price of an input token and a fiftieth of an output token.
So that number largely tracks how long the conversation has got, not how much
allowance has been spent. It read 94% while the account's real session usage
was 30%.

**Wrong answer two: calibrate the ceiling from history.** Even with a better
metric, "a full session is the second-heaviest one on record" cannot survive a
change of plan or, as happened here, a temporary 50% limit boost. The history
was recorded under a different limit.

**The right answer: ask.** Claude Code's `/usage` screen reads
`/api/oauth/usage` with the OAuth token from `~/.claude/.credentials.json`,
and returns `five_hour`, `seven_day` and `overage` buckets, each with a
`utilization` and a `resets_at`. That is the real number.

Two things make it awkward, and the design is mostly a response to them:

- **It is severely rate limited.** Two probes in quick succession earned a
  1579-second `Retry-After`. So it is polled every 15 minutes at most, the
  server's figure always overrides the configured interval, and any other
  failure backs off geometrically to a 2-hour ceiling.
- **It is undocumented.** The parser is deliberately loose where the
  convention cannot be pinned — utilization is accepted as a percentage or a
  fraction, `resets_at` as RFC3339 or a unix timestamp in either unit — and
  every failure maps to `ErrUnavailable`, which means "fall back", never
  "stop".

The token is also short-lived (hours) and refreshed in place by Claude Code,
so the credential file is re-read on **every** request. Caching it would mean
the daemon quietly stopped working a few hours after launch while a valid
token sat on disk.

### Anchoring

A real reading is an anchor: utilization, reset time, and the local cost meter
at that instant. Between anchors, only the *delta* in local cost is added, so
a mismatch between the two sources' window boundaries cannot accumulate. When
the session resets, the anchor is dropped and the display waits for the next
real reading.

The anchor also calibrates the fallback, which is the part that makes it
trustworthy: dividing local cost by real utilization reveals what a full
session actually costs on this account and plan. If the endpoint later breaks,
the estimate that takes over is one the real data taught it, not a guess from
history. A reading below 10% is refused for this purpose -- dividing a few
dollars by 2% implies almost any ceiling at all.

The projection warning is a separate signal: when the current burn rate points
past the ceiling before the session resets, every band above the real level
glows, and how hard it glows scales with the size of the overshoot.

### Three layers of motion, three meanings

Each moving thing on the wall says exactly one thing, which is why they are
separate mechanisms rather than one tunable effect:

| Layer | Means | Shape |
|---|---|---|
| Ambient wave | the display is alive | a narrow highlight sweeping the long axis, when idle |
| Rainbow | Claude is working | hue sweeping the shape, faster for a tool call than for thinking |
| Overrun blink | the rate is heading past the limit | the unspent bands glowing in place |

The ambient wave is a narrow highlight that travels the long axis, and it
intensifies rather than whitens: a small lift in lightness, with the headroom
that leaves spent on chroma instead of on white.

Both of those choices are corrections of earlier attempts, and each fixed one
complaint while creating the next:

1. **A sine modulating everything** had to pale every panel at once, because a
   bright highlight loses chroma to stay in gamut. A washed-out green and a
   washed-out red look much the same, which defeats the only thing the
   gradient is for. Hence a narrow highlight: two or three bands at a time,
   the rest at their exact colour.
2. **Adding light** was still the wrong currency. Light and chroma trade
   against each other, so a strong lift forces any hue towards white. Keeping
   the lift small and spending the headroom on chroma makes the highlight read
   as the band's own colour turned up: the green goes to a pure (0,241,0)
   instead of a pale (188,222,179).

What cannot be fixed is that the gamut is not the same shape at every hue. A
green at this lightness has chroma to spare and gains 66%; a bright red has
none and loses a third of its own. So the highlight is uneven by nature -- the
test asserts only that no part of the scale is left without one, which is the
property that actually matters.

One trap this exposed, worth knowing before touching these tests: **saturating
a colour can lower its channel sum**. A pure (0,241,0) sums to less than the
(105,189,76) it came from while being obviously more vivid, so neither end of
the highlight sweep is reliably the brightest. Any threshold has to be
measured across the sweep rather than taken from an endpoint. Its length is a little over half the shape, so more than one crest is
visible at once and it reads as travelling rather than as the whole figure
pulsing.

Three things about it were arrived at by measurement rather than by taste,
and all three are counter-intuitive:

**It looked choppy for a reason that had nothing to do with frame rate.** The
first version dimmed by 14% over a 7-second cycle and appeared to run at about
one frame a second. The panels were verifiably receiving 20 -- 100 UDP packets
in 5 seconds -- but at that depth an 8-bit channel takes only 14 distinct
values across the whole cycle, so each one is held for a quarter of a second.
What buys smooth motion is contrast per unit time, not more frames.

**Draining chroma is what makes it visible on green.** A pure dimming is
almost perfectly uniform in measured lightness across the ramp -- OKLab ΔL
between 0.107 and 0.132 on all nine bands -- yet it reads as much weaker on
the greens, which is where the eye's luminance sensitivity peaks. Taking some
colour as well lands evenly on every hue: measured in bytes that actually
reach the panels, the spread across the ramp went from 212-283 down to
163-222, and the greens now move *more* than the reds rather than less.

Draining chroma also solved the first problem, which is why the wave can be
slow. Desaturating moves the three channels by different amounts, so the
output takes far more distinct values than a pure scale: at a 9-second period
it produces 111-119 distinct colours per band, around 13 perceptible changes
a second -- slower than the 3.5-second version it replaced and smoother than
it too.

**It looked like it affected every panel at once because something else did.**
There used to be a whole-shape brightness breath under `PhaseIdle`, scaling
every panel identically between 0.72 and 1.0 -- and on the same nine-second
period as the wave, so it completely masked the spatial one. It is gone. The
wave is now the only ambient motion, and being a function of position it is
progressive by construction: at any instant the nine panels sit anywhere
between 65% and 100% of their own colour.

That left the ramp itself as the last source of unevenness, and it was the
biggest: see the constant-lightness ramp below.

## Calibration happens in a browser

Nothing tells the device which way is up in the room. `/panelLayout` gives
relative positions and a `globalOrientation`, and neither of them knows where
the floor is. Somebody has to look at the wall and say. That turned out to be
the hardest interface in the project, and it took three attempts.

**Attempt one: ask for the angle.** `NANOCLAUDE_ROTATION`, in degrees,
documented. It is not something a person can produce: working out that this
wall wanted 0 rather than 116 took a photograph, a PCA fit over the panel
centroids and a check against a third point. Configuration that can only be
filled in by the author is not configuration.

**Attempt two: a dial in the terminal.** Arrow keys turn the shape, the panels
follow, enter saves. Much better — nobody names an angle — but the shape on
screen is coloured blocks on a character grid, so the thing being compared
against the wall is already a translation of it. The verdict on it was one
sentence long, and it was right.

**Attempt three: a page on 127.0.0.1.** A browser can draw the actual
triangles at their actual angles, and a mouse can turn them. That is the whole
argument. `internal/webui` serves it and `nanoclaude calibrate` opens it.

### The browser does no geometry

Every polygon the page draws arrives from Go, in screen coordinates, together
with the colours that went to the panels in the same frame. The page sets
attributes; it does not compute positions. A calibration tool whose screen and
wall could disagree would be worse than no tool at all, and two
implementations of one projection is exactly the bug this project already paid
for once — the sign of `globalOrientation` was wrong for a while, and a second
copy of the transform would have hidden the fix.

That is why `render.Project` exists. `FromLayout` is built on it, so the
renderer and the drawing share one rotation rather than each applying their
own.

### The outlines were measured, not assumed

A triangle's corners sit at `90° + o + 120k` from its centroid, at a
circumradius of `side/√3`. The base angle came from a real NL42 rather than
from a convention: on that device the panels reporting `o=0` point up and the
ones reporting `o=180` point down, and their centroids are offset by half a
side across and one inradius up, which is a triangular tiling of the reported
side length and nothing else. `TestTrianglesShareTheirEdges` pins it the way a
wall does: panels that touch in the room must share two corners in the
drawing. A base angle 60° out makes neighbours share none, and the page then
draws a pile of overlapping shapes.

Hexagons have no device here to check against, so their corner angle is
*inferred* rather than picked: hexagons meet edge to edge, so the direction
from one to its nearest neighbour points at the middle of a shared edge, and a
corner is 30° from that. Guessing between flat-top and pointy-top would be
wrong half the time, and wrong by a twelfth of a turn looks like a fault
rather than a choice.

Mini triangles are the one guess left. A device reports a single side length
and a mixed set has two, so a mini triangle is halved only when full triangles
are present as well. In a mini-only set the reported length already is the
mini one, and halving it would draw a tiling full of gaps.

### The drawing must not breathe

The frame the shape is drawn in is a square whose half-width never changes
with the rotation. Measured from the shape's bounding box it would: an
arrangement wider than tall fills more of a square at 90° than at 0°, so the
drawing would zoom in and out as the mouse moved, which reads as the page
being broken rather than as the shape turning. The bound is built only from
distances to the centre of rotation, which no rotation can change.
`TestExtentSurvivesRotation` holds it.

### It is a control surface, not a viewer

The page changes what a wall of lights is doing and writes to the user's
configuration file, so reaching it has to mean something:

- **Loopback only.** There is deliberately no flag to bind elsewhere.
- **Two codes, not one.** The address printed in the terminal carries a
  *ticket* that works exactly once and redirects to the session's real token,
  which is the first path segment of every route and is compared in constant
  time. The reason is that the address has to be handed to a browser as a
  command-line argument, and on Linux `/proc/<pid>/cmdline` is world readable:
  another user on the machine can read the browser's arguments for as long as
  it runs. A single code there would be readable by anyone on the box, and this
  page repaints a wall and writes a configuration file. A spent ticket buys
  them nothing, and spending it first is a race they can only win by being
  noticed, because the user's own browser then says the address has already
  been opened.
- **The `Host` header must be this server's own loopback address.** Without
  that check, a name in someone else's DNS pointed at 127.0.0.1 makes their
  page same-origin with this one, which is the whole DNS rebinding attack.
- **`Sec-Fetch-Site` and `Origin`**, so another site the user happens to have
  open cannot post here.
- **A content security policy that allows nothing but this origin**, which is
  also why the page loads no CDN and embeds no third-party code.

### The panels are handed back once, and not taken again

Enabling streaming mode is the only step that changes the device and cannot be
undone, because the effect it replaces is known only to whoever saved it. Two
things follow, and both were originally wrong here:

The session opens the stream *last*. The port, the layout and the
configuration path are all settled by `webui.New` before `Run` touches the
device, so an address already in use no longer costs the user their effect.

And nothing may take the panels back afterwards. The loop that paints them
runs in its own goroutine, so `Run` waits for it before returning, and the
stream refuses to reopen once it has been closed. Without both, a loop one
tick behind the shutdown would re-enable streaming mode *after* the effect had
been restored, leaving the panels stuck in it with the saved effect already
spent — unrecoverable, and from the user's side indistinguishable from the
program simply breaking their lights.

### The tab is the session

The event stream is the presence signal. When the last page disconnects the
session ends on its own and the panels are handed back, because a calibration
pattern left on the wall by a forgotten terminal is a bug the user cannot
diagnose. Before any page has connected the wait is longer, since a cold
browser can take a while to start.

The other thing a session has to survive is a fight. The device leaves
streaming mode whenever anything else selects an effect on it — the Nanoleaf
app, a home automation, or the display itself starting up from a hook in the
middle of a calibration — and the next datagram comes back refused. So a
failed send reopens the stream and says so on the page, and only a device that
keeps refusing for ten seconds ends the session. The daemon does the same
thing for the same reason.

### Sample shapes, because every wall is different

The arrangement this was written against is nine triangles in a diagonal
zigzag. Nothing about the drawing, the bands or the long axis is allowed to
depend on that, and the only way to know is to look at other walls.
`internal/shapes` holds nine of them, built from the tilings the real products
click together in: honeycombs, grids, rows, blocks, two sizes of triangle
mixed, and the original Light Panels. `nanoclaude preview --web` serves the
same page over them with no device attached, so there is nothing to paint and
no angle worth saving — the page hides its Save button and says so.

The samples are also what the outlines are measured against.
`TestEverySampleShapeTiles` holds every one of them to the property a real
wall has: two panels of the same kind that sit closest to each other are edge
to edge, so their outlines share exactly two corners. A corner angle out by a
sixth of a turn on triangles, a twelfth on hexagons or an eighth on squares
all fail it, and all three would otherwise draw a heap of overlapping shapes
that only a person looking at the page would notice.

Mixed sets are the one place the drawing still guesses. A device reports a
single side length and a set holding both sizes of triangle has two, so a mini
is halved only when full triangles are present as well.

### Vanilla, embedded, no build step

The house frontend stack is SvelteKit, and this is the one place it is
deliberately not used. The deploy target is a Go binary, and
`go install .../cmd/nanoclaude@latest` has to remain the entire installation.
A framework would mean either a JavaScript toolchain in front of the binary or
build output committed to the repository, and this page is one SVG, five
controls and 300 lines of DOM code. `go:embed` costs nothing and keeps the
supply chain at zero.

## Lifetime

The daemon is started by Claude Code, not by a service manager. `SessionStart`
runs `nanoclaude up`, which spawns it detached and returns immediately; it
stands down by itself after 20 minutes with no live session. So the display
exists while Claude Code does, and there is nothing to enable, autostart, or
remember.

Three things make that safe:

- **A single-instance flock.** The hook fires once per session, so three
  worktrees would otherwise start three daemons all streaming different
  frames to the same panels at 20fps. An advisory lock is used rather than a
  PID file because the kernel releases it however the process dies -- a PID
  file left by a crash would block every future start until someone deleted
  it.
- **Every hook revives it, not just `SessionStart`.** Without that, idle
  shutdown would be a trap: a session left open long enough for its state
  file to go stale never fires `SessionStart` again, so the display could
  never come back. The revival check is a file open and a non-blocking flock
  -- about a millisecond, on a hook that already costs two.
- **It reads its own config file.** Started by a hook it inherits Claude
  Code's environment, which knows nothing about panels or tokens. Depending on
  the launcher to supply them worked only while systemd was doing it.

The systemd unit is still in `deploy/`, and is the better choice if the
display should outlive Claude Code: it adds restart-on-failure and a real
sandbox. Set `NANOCLAUDE_IDLE_EXIT=0` there.

## Package layout

| Package | Why there |
|---|---|
| `pkg/nanoleaf` | A protocol client — HTTP plus the UDP frame format. Genuinely reusable; there is no good Go client for this API. |
| `internal/usage` | Launches `ccusage` and finds it on `PATH`. Touching the OS is not library behaviour. |
| `internal/limits` | The real usage endpoint. Internal because it depends on an undocumented API and on Claude Code's credential store: not something to invite others to import. |
| `internal/activity` | Hook state files: this daemon's private contract with its own hook command. |
| `internal/hass` | Read-only Home Assistant client, scoped to the one entity this needs. |
| `internal/render` | Geometry and colour. No I/O, which is why it is the best-tested package. `FromLayout` is the only conversion production code should use — it also drops panels whose ID will not fit the protocol's 16-bit field, since one bad ID makes the device reject every frame whole. |
| `internal/daemon` | Wiring and the loop. |
| `internal/discover` | Sweeps the local networks for controllers. Scans rather than using mDNS, because reaching an mDNS advert needs a resolver running locally and that is not a safe assumption: on the machine this was written on, avahi-daemon was stopped while the panels answered fine. A /24 takes under a second. |
| `internal/hooks` | Registers the Claude Code hooks. Edits `settings.json` in place with a JSON path library rather than decoding and re-encoding it, because Go marshals a map with its keys sorted and the file is hand-maintained: a re-encode would reshuffle 26 KB of configuration to add nine entries. |
| `internal/shapes` | Sample arrangements, built from the tilings the real panels click together in. They exist because a display that only reads well on one wall is not finished, and because the outlines have to be measured against something other than the author's own wall. |
| `internal/webui` | The calibration page. Everything it draws is computed in Go and sent to it, so the picture on screen and the picture on the wall come from one frame. Embedded with `go:embed`: the deploy target is a Go binary, so `go install` must stay the whole installation. |
| `internal/ui` | The shared styles, and the check for whether output is a terminal. Every live view has a plain counterpart, because a progress bar written to a pipe is a stream of escape codes. |
