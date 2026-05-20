"""Amp hook adapter stub for the public dotvault contract."""

from __future__ import annotations

import json
import os
import sys


def main() -> int:
    json.load(sys.stdin)
    event = os.environ.get("DOTVAULT_HOOK_EVENT", "unknown")
    print(f"dotvault amp adapter: no-op for {event}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
