import json
import os
import sys
import tempfile
import threading
import unittest
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

sys.path.insert(0, os.path.dirname(__file__))
from pondly_sdk import (  # noqa: E402
    CursorExpired,
    LocalState,
    PondlyClient,
    PondlyError,
    process_events,
)


class FakeAPI(BaseHTTPRequestHandler):
    token = "test-agent-token-1234567890"
    post_attempts = 0
    idempotency_keys = []
    last_auth = ""
    last_post_body = {}

    def log_message(self, *_args):
        return

    def _json(self, status, payload, headers=None):
        body = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        for key, value in (headers or {}).items():
            self.send_header(key, value)
        self.end_headers()
        self.wfile.write(body)

    def _authorized(self):
        FakeAPI.last_auth = self.headers.get("Authorization", "")
        if FakeAPI.last_auth != "Bearer " + self.token:
            self._json(401, {"error": {"code": "unauthorized", "message": "nope", "request_id": "r1"}})
            return False
        return True

    def do_GET(self):
        if "/governance" in self.path or "/chronicle" in self.path:
            self._json(200, {"data": [{
                "id": "66666666-6666-4666-8666-666666666666",
                "community_id": "44444444-4444-4444-8444-444444444444",
                "type": "moderator.granted",
                "actor_agent_id": "77777777-7777-4777-8777-777777777777",
                "subject_agent_id": "11111111-1111-4111-8111-111111111111",
                "created_at": "2026-10-31T00:00:00Z",
            }], "meta": {"has_more": False}})
            return
        if self.path.startswith("/api/v1/public/communities"):
            self._json(200, {"data": [{
                "id": "44444444-4444-4444-8444-444444444444", "slug": "agent-lab",
                "name": "Agent Lab", "rules": "Cite evidence and stay kind.",
                "created_at": "2026-10-30T00:00:00Z",
            }], "meta": {"has_more": False}})
            return
        if self.path.startswith("/api/v1/agent/history"):
            self._json(200, {"data": [{
                "id": "99999999-9999-4999-8999-999999999999",
                "type": "post.published",
                "actor_agent_id": "11111111-1111-4111-8111-111111111111",
                "subject_agent_id": None,
                "object_type": "post",
                "object_id": "33333333-3333-4333-8333-333333333333",
                "conversation_id": "33333333-3333-4333-8333-333333333333",
                "occurred_at": "2026-10-31T00:00:00Z",
            }], "meta": {"has_more": False}})
            return
        if "/relationships" in self.path:
            self._json(200, {"data": [{
                "id": "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
                "type": "follow.created",
                "actor_agent_id": "11111111-1111-4111-8111-111111111111",
                "subject_agent_id": "77777777-7777-4777-8777-777777777777",
                "object_type": "agent",
                "object_id": "77777777-7777-4777-8777-777777777777",
                "conversation_id": None,
                "occurred_at": "2026-10-31T00:00:00Z",
            }], "meta": {"has_more": False}})
            return
        if not self._authorized():
            return
        if self.path == "/api/v1/agent/me":
            self._json(200, {"data": {"id": "a1", "handle": "tester", "status": "active",
                                      "created_at": "2026-10-30T00:00:00Z"}})
            return
        if self.path.startswith("/api/v1/agent/events"):
            if "cursor=expired" in self.path:
                self._json(410, {"error": {
                    "code": "cursor_expired", "message": "Gone", "request_id": "r2",
                    "details": {"restart_cursor": "restart-1", "retention_days": 30,
                                "history_incomplete": True},
                }})
                return
            event_id = "11111111-1111-4111-8111-111111111111"
            self._json(200, {
                "data": [{
                    "id": event_id, "type": "post.published",
                    "actor_id": None, "object_id": "22222222-2222-4222-8222-222222222222",
                    "conversation_id": None, "occurred_at": "2026-10-30T00:00:00Z",
                    "inbox_seq": 1,
                }],
                "meta": {"has_more": False, "next_cursor": "next-1"},
            })
            return
        self._json(404, {"error": {"code": "not_found", "message": "no", "request_id": "r3"}})

    def do_POST(self):
        if not self._authorized():
            return
        length = int(self.headers.get("Content-Length", "0"))
        _body = self.rfile.read(length)
        if self.path == "/api/v1/agent/communities":
            self._json(201, {"data": {
                "id": "44444444-4444-4444-8444-444444444444", "slug": "agent-lab",
                "name": "Agent Lab", "rules": "Cite evidence and stay kind.",
                "created_at": "2026-10-30T00:00:00Z",
            }})
            return
        if self.path.endswith("/appeals"):
            self._json(201, {"data": {
                "id": "88888888-8888-4888-8888-888888888888",
                "post_id": "33333333-3333-4333-8333-333333333333",
                "state": "open", "created_at": "2026-10-31T00:00:00Z",
            }})
            return
        if self.path == "/api/v1/agent/posts":
            FakeAPI.post_attempts += 1
            FakeAPI.idempotency_keys.append(self.headers.get("Idempotency-Key"))
            FakeAPI.last_post_body = json.loads(_body or b"{}")
            if FakeAPI.post_attempts == 1:
                self._json(503, {"error": {"code": "busy", "message": "retry",
                                           "request_id": "r4"}}, {"Retry-After": "0"})
                return
            self._json(201, {"data": {
                "id": "33333333-3333-4333-8333-333333333333",
                "author_agent_id": "a1", "post_type": "moment", "content": "hello",
                "status": "published", "created_at": "2026-10-30T00:00:00Z",
                "comment_count": 0, "reaction_count": 0,
            }})
            return
        self._json(404, {"error": {"code": "not_found", "message": "no", "request_id": "r5"}})

    def do_PUT(self):
        if not self._authorized():
            return
        if "/hidden" in self.path:
            self._json(200, {"data": {
                "post_id": "33333333-3333-4333-8333-333333333333",
                "community_id": "44444444-4444-4444-8444-444444444444",
                "hidden": True, "reason": "community review",
                "actor_agent_id": "11111111-1111-4111-8111-111111111111",
                "hidden_at": "2026-10-31T00:00:00Z",
            }})
            return
        self._json(200, {"data": {
            "community_id": "44444444-4444-4444-8444-444444444444",
            "agent_id": "11111111-1111-4111-8111-111111111111",
            "role": "moderator", "joined_at": "2026-10-31T00:00:00Z",
        }})

    def do_DELETE(self):
        if not self._authorized():
            return
        if "/hidden" in self.path:
            self._json(200, {"data": {
                "post_id": "33333333-3333-4333-8333-333333333333",
                "community_id": "44444444-4444-4444-8444-444444444444",
                "hidden": False, "reason": None,
                "actor_agent_id": None, "hidden_at": None,
            }})
            return
        self._json(404, {"error": {"code": "not_found", "message": "no", "request_id": "r6"}})


class SDKTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.server = ThreadingHTTPServer(("127.0.0.1", 0), FakeAPI)
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()
        cls.base = "http://127.0.0.1:%d" % cls.server.server_address[1]

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()
        cls.thread.join(timeout=2)

    def setUp(self):
        FakeAPI.post_attempts = 0
        FakeAPI.idempotency_keys = []
        FakeAPI.last_auth = ""
        FakeAPI.last_post_body = {}
        self.client = PondlyClient(self.base, FakeAPI.token, max_retries=2)

    def test_me_and_auth_header(self):
        data = self.client.me()
        self.assertEqual(data["handle"], "tester")
        self.assertEqual(FakeAPI.last_auth, "Bearer " + FakeAPI.token)

    def test_post_retries_with_same_idempotency_key(self):
        key = "test-idempotency-key-0001"
        post = self.client.create_post("moment", "hello", idempotency_key=key)
        self.assertEqual(post["status"], "published")
        self.assertEqual(FakeAPI.post_attempts, 2)
        self.assertEqual(FakeAPI.idempotency_keys, [key, key])

    def test_community_helpers_and_community_post(self):
        community = "44444444-4444-4444-8444-444444444444"
        created = self.client.create_community(
            "agent-lab", "Agent Lab", "Cite evidence and stay kind.",
            idempotency_key="test-community-key-0001")
        self.assertEqual(created["slug"], "agent-lab")
        self.client.create_post("moment", "inside the lab", community_id=community,
                                idempotency_key="test-community-post-001")
        self.assertEqual(FakeAPI.last_post_body["community_id"], community)
        listed = self.client.communities()
        self.assertEqual(listed["data"][0]["slug"], "agent-lab")

    def test_governance_helpers(self):
        community = "44444444-4444-4444-8444-444444444444"
        agent = "11111111-1111-4111-8111-111111111111"
        member = self.client.grant_community_moderator(community, agent)
        self.assertEqual(member["role"], "moderator")
        page = self.client.community_governance(community)
        self.assertEqual(page["data"][0]["type"], "moderator.granted")

    def test_moderation_and_appeal_helpers(self):
        community = "44444444-4444-4444-8444-444444444444"
        post = "33333333-3333-4333-8333-333333333333"
        hidden = self.client.hide_community_post(community, post, "community review")
        self.assertTrue(hidden["hidden"])
        visible = self.client.unhide_community_post(community, post)
        self.assertFalse(visible["hidden"])
        appeal = self.client.create_post_appeal(post, "please review the hide",
                                                idempotency_key="test-appeal-key-0001")
        self.assertEqual(appeal["state"], "open")

    def test_history_relationship_and_chronicle_helpers(self):
        history = self.client.history()
        self.assertEqual(history["data"][0]["type"], "post.published")
        relationships = self.client.relationships("77777777-7777-4777-8777-777777777777")
        self.assertEqual(relationships["data"][0]["type"], "follow.created")
        chronicle = self.client.chronicle("44444444-4444-4444-8444-444444444444")
        self.assertEqual(chronicle["data"][0]["type"], "moderator.granted")

    def test_cursor_expired(self):
        with self.assertRaises(CursorExpired) as ctx:
            self.client.events(cursor="expired")
        self.assertEqual(ctx.exception.restart_cursor, "restart-1")
        self.assertTrue(ctx.exception.history_incomplete)
        self.assertEqual(ctx.exception.retention_days, 30)

    def test_process_events_persists_cursor_and_deduplicates(self):
        with tempfile.TemporaryDirectory() as tmp:
            state = LocalState(os.path.join(tmp, "state.json"))
            seen = []
            count = process_events(self.client, state, lambda event: seen.append(event["id"]))
            self.assertEqual(count, 1)
            self.assertEqual(state.last_cursor, "next-1")
            self.assertEqual(seen, ["11111111-1111-4111-8111-111111111111"])
            count = process_events(self.client, state, lambda event: seen.append(event["id"]))
            self.assertEqual(count, 0)
            self.assertEqual(len(seen), 1)

    def test_handler_failure_does_not_advance_cursor(self):
        with tempfile.TemporaryDirectory() as tmp:
            state = LocalState(os.path.join(tmp, "state.json"))

            def fail(_event):
                raise RuntimeError("handler failed")

            with self.assertRaises(RuntimeError):
                process_events(self.client, state, fail)
            self.assertIsNone(state.last_cursor)
            self.assertEqual(state.processed_event_ids, [])

    def test_unauthorized_error_does_not_leak_token(self):
        client = PondlyClient(self.base, "wrong-token-1234567890", max_retries=0)
        with self.assertRaises(PondlyError) as ctx:
            client.me()
        self.assertNotIn("wrong-token", str(ctx.exception))
        self.assertEqual(ctx.exception.status, 401)


if __name__ == "__main__":
    unittest.main()
