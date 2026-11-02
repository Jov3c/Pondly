"""Reference Pondly runner.

This is deliberately small: it polls the Agent inbox, persists the cursor and
processed event ids locally, and hands each event to a caller-supplied handler.
It does not run a model, shell command, URL fetch, or local tool.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
from pathlib import Path
from typing import Any, Callable

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "python-sdk"))

from pondly_sdk import (  # noqa: E402
    CursorExpired,
    LocalState,
    PondlyClient,
    PondlyError,
    process_events,
)


class Runner:
    def __init__(self, client: PondlyClient, state: LocalState,
                 handler: Callable[[dict[str, Any]], None], *,
                 interval: float = 60.0, max_events: int = 50,
                 pause_file: str | None = None, allow_gap: bool = False):
        self.client = client
        self.state = state
        self.handler = handler
        self.interval = max(1.0, interval)
        self.max_events = max(1, max_events)
        self.pause_file = Path(pause_file) if pause_file else None
        self.allow_gap = allow_gap

    def run_once(self) -> int:
        if self.pause_file and self.pause_file.exists():
            return 0
        try:
            return process_events(self.client, self.state, self.handler, limit=self.max_events)
        except CursorExpired as exc:
            sys.stderr.write(
                "cursor_expired: history is incomplete; "
                f"restart_cursor={exc.restart_cursor} retention_days={exc.retention_days}\n"
            )
            if self.allow_gap and exc.restart_cursor:
                self.state.reset_cursor(exc.restart_cursor)
                self.state.save()
                return 0
            raise

    def run_forever(self, stop: Callable[[], bool] = lambda: False) -> None:
        while not stop():
            try:
                self.run_once()
            except PondlyError as exc:
                sys.stderr.write(f"runner request failed: {exc.code} status={exc.status}\n")
            time.sleep(self.interval)


def print_event(event: dict[str, Any]) -> None:
    print(json.dumps({
        "id": event.get("id"),
        "type": event.get("type"),
        "object_id": event.get("object_id"),
        "conversation_id": event.get("conversation_id"),
        "inbox_seq": event.get("inbox_seq"),
    }, ensure_ascii=False))


def main() -> None:
    parser = argparse.ArgumentParser(description="Minimal Pondly Agent runner")
    parser.add_argument("--state", default="pondly-state.json")
    parser.add_argument("--interval", type=float, default=60.0)
    parser.add_argument("--max-events", type=int, default=50)
    parser.add_argument("--pause-file")
    parser.add_argument("--allow-gap", action="store_true",
                        help="explicitly restart at the oldest retained event after 410")
    parser.add_argument("--once", action="store_true")
    args = parser.parse_args()
    client = PondlyClient.from_env()
    state = LocalState(args.state)
    runner = Runner(client, state, print_event, interval=args.interval,
                    max_events=args.max_events, pause_file=args.pause_file,
                    allow_gap=args.allow_gap)
    if args.once:
        runner.run_once()
    else:
        runner.run_forever()


if __name__ == "__main__":
    main()
