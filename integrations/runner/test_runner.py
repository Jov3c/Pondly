import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(__file__))
from runner import Runner  # noqa: E402
from pondly_sdk import CursorExpired, LocalState  # noqa: E402


class FakeClient:
    def __init__(self, page):
        self.page = page

    def events(self, *, cursor=None, limit=50):
        return self.page


class RunnerTests(unittest.TestCase):
    def test_handles_events_and_pause(self):
        page = {"data": [{"id": "e1", "type": "post.published"}],
                "meta": {"has_more": False, "next_cursor": "c1"}}
        with tempfile.TemporaryDirectory() as tmp:
            state = LocalState(os.path.join(tmp, "state.json"))
            seen = []
            runner = Runner(FakeClient(page), state, lambda event: seen.append(event["id"]))
            self.assertEqual(runner.run_once(), 1)
            self.assertEqual(seen, ["e1"])
            self.assertEqual(state.last_cursor, "c1")
            pause = os.path.join(tmp, "pause")
            open(pause, "w", encoding="utf-8").close()
            paused = Runner(FakeClient(page), state, lambda event: seen.append(event["id"]),
                            pause_file=pause)
            self.assertEqual(paused.run_once(), 0)
            self.assertEqual(len(seen), 1)

    def test_cursor_gap_requires_explicit_recovery(self):
        class GapClient:
            def events(self, **_kwargs):
                raise CursorExpired({"restart_cursor": "restart", "retention_days": 30,
                                     "history_incomplete": True}, "r")

        with tempfile.TemporaryDirectory() as tmp:
            state = LocalState(os.path.join(tmp, "state.json"))
            with self.assertRaises(CursorExpired):
                Runner(GapClient(), state, lambda _event: None).run_once()
            recovering = Runner(GapClient(), state, lambda _event: None, allow_gap=True)
            self.assertEqual(recovering.run_once(), 0)
            self.assertEqual(state.last_cursor, "restart")


if __name__ == "__main__":
    unittest.main()
