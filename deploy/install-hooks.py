#!/usr/bin/env python3
"""Register nanoclaude's activity hooks in Claude Code's settings.

Appends one hook entry per event, leaving every existing entry untouched --
other tools (Orca, for one) register their own hooks on the same events, and
an install that replaced them would break them.

Idempotent: running it twice does not duplicate anything. Pass --remove to
undo. A timestamped backup is written before any change.
"""

import argparse
import json
import os
import shutil
import sys
import time
from pathlib import Path

# The events the display reacts to. PreToolUse/PostToolUse are matched against
# every tool, hence the "*" matcher; the session-level events take none.
EVENTS_WITH_MATCHER = [
    "PreToolUse",
    "PostToolUse",
    "PostToolUseFailure",
    "PermissionRequest",
]
EVENTS_WITHOUT_MATCHER = [
    "UserPromptSubmit",
    "Stop",
    "StopFailure",
    "SessionEnd",
]

# SessionStart both records the session and brings the display up, so the
# daemon needs no service manager and no autostart entry: it exists while
# Claude Code does. Every other hook also revives it, which is what makes the
# daemon safe to stand down when idle -- a session left open long enough for
# its state to go stale never fires SessionStart again, but it does fire the
# rest.
STARTUP_EVENT = "SessionStart"

# Key stamped into our own entries so they can be recognised on reinstall and
# removal. A substring match on the command would break idempotency the moment
# the binary is installed under a different name -- every run would append
# another copy.
MARKER_KEY = "_nanoclaude"


def hook_entry(binary: str, with_matcher: bool, startup: bool = False) -> dict:
    # A short timeout because this runs on the critical path of every tool
    # call; the binary writes one small file and exits.
    commands = [{"type": "command", "command": f"{binary} hook", "timeout": 5}]
    if startup:
        # `up` starts the daemon detached and returns; it does not wait for
        # it, so the session is never held up by the display.
        commands.append({"type": "command", "command": f"{binary} up", "timeout": 10})

    entry = {MARKER_KEY: True, "hooks": commands}
    if with_matcher:
        entry["matcher"] = "*"
    return entry


def is_ours(entry: dict) -> bool:
    if entry.get(MARKER_KEY):
        return True
    # Recognise entries written before the marker existed, so an upgrade does
    # not duplicate them.
    return any("nanoclaude hook" in h.get("command", "") for h in entry.get("hooks", []))


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument(
        "--settings",
        default=str(Path.home() / ".claude" / "settings.json"),
        help="path to Claude Code settings.json",
    )
    ap.add_argument(
        "--binary",
        default=str(Path.home() / ".local" / "bin" / "nanoclaude"),
        help="absolute path to the nanoclaude binary",
    )
    ap.add_argument("--remove", action="store_true", help="remove the hooks instead")
    ap.add_argument("--dry-run", action="store_true", help="print the change and exit")
    args = ap.parse_args()

    path = Path(args.settings)
    if not path.exists():
        print(f"error: {path} does not exist", file=sys.stderr)
        return 1

    settings = json.loads(path.read_text())
    hooks = settings.setdefault("hooks", {})

    changed = []
    for event in EVENTS_WITH_MATCHER + EVENTS_WITHOUT_MATCHER + [STARTUP_EVENT]:
        entries = hooks.setdefault(event, [])
        existing = [e for e in entries if is_ours(e)]

        if args.remove:
            if existing:
                hooks[event] = [e for e in entries if not is_ours(e)]
                changed.append(f"-{event}")
            continue

        if existing:
            continue
        entries.append(
            hook_entry(
                args.binary,
                with_matcher=event in EVENTS_WITH_MATCHER,
                startup=event == STARTUP_EVENT,
            )
        )
        changed.append(f"+{event}")

    if not changed:
        print("nothing to do: hooks already in the desired state")
        return 0

    print("changes:", " ".join(changed))
    if args.dry_run:
        return 0

    backup = path.with_suffix(f".json.bak-{time.strftime('%Y%m%d-%H%M%S')}")
    shutil.copy2(path, backup)
    print(f"backup: {backup}")

    # Written to a temp file in the same directory and renamed into place.
    # A crash midway through a plain write would truncate Claude Code's
    # settings.json -- there is a backup, but losing the live file over a wall
    # light is not a trade worth making.
    payload = json.dumps(settings, indent=2, ensure_ascii=False) + "\n"
    tmp = path.with_suffix(f".json.tmp-{os.getpid()}")
    try:
        tmp.write_text(payload)
        os.replace(tmp, path)
    finally:
        tmp.unlink(missing_ok=True)
    print(f"updated: {path}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
