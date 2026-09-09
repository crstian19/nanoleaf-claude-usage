# nanoclaude

A daemon that renders Claude Code token usage onto Nanoleaf wall panels.
Read `docs/architecture.md` before changing rendering or the wire protocol.

## Conventions

- `internal/` by default. `pkg/` only for genuine library code: `pkg/nanoleaf`
  is a protocol client, while anything that launches processes or resolves
  binaries on `PATH` stays internal.
- `net/http` from the standard library. No HTTP wrappers.
- One responsibility per file.
- Comments explain *why*, not *what*.
- `just ci` (lint, race tests, `govulncheck`) must pass before a commit.

## Things that will bite you

- **The Nanoleaf auth token travels in the URL path.** Every error and log
  line that could contain a URL must go through `redactPath`.
- **Panel colours are sent over UDP with no acknowledgement.** A send error
  means a broken socket, not a rejected frame.
- **`nanoclaude hook` runs on the critical path of every tool call.** It must
  stay fast and must never fail in a way the user's session notices.
- **Rendering is geometric.** Panels are points on a wall, not a list of
  cells. Do not add code that assumes panel order or a fixed panel count.
- **Two colour spaces.** `Lerp` interpolates in OKLab because that is a
  perceptual question; `Scale` and `Add` stay in linear light because
  brightness and summing light sources are physical ones. Do not unify them.
- **Enabling extControl is the only destructive call.** Anything that can
  fail must fail before it, or the user's effect is lost for good.
- **The calibration page computes nothing.** Every coordinate and colour it
  draws comes from Go, from the same frame that went to the panels. A page
  that could disagree with the wall is worse than no page, and two copies of
  one projection is how the `globalOrientation` sign bug survived as long as
  it did.
- **Verify module versions with `go list -m -versions`**, not from memory. The
  Charm v2 modules live under `charm.land/`, but `fang` does not.
