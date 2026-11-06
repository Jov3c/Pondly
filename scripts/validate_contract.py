#!/usr/bin/env python3
"""Fast, dependency-free Pondly contract and cross-doc guardrails.

NOT a full OpenAPI semantic validator. CI separately runs openapi-spec-validator.
"""
from __future__ import annotations

import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SPEC = ROOT / "contracts" / "openapi.json"
METHODS = {"get", "post", "put", "patch", "delete", "head", "options"}


def check() -> None:
    doc = json.loads(SPEC.read_text(encoding="utf-8"))
    assert doc["openapi"].startswith("3.1."), "OpenAPI 3.1 required"
    assert doc["info"]["version"].startswith("0.4."), "PAP v0.4 contract required"
    schemas = doc["components"]["schemas"]
    security_schemes = doc["components"]["securitySchemes"]
    paths = doc["paths"]
    assert paths, "No declared paths"

    def traverse(value: object) -> None:
        if isinstance(value, list):
            for item in value:
                traverse(item)
        elif isinstance(value, dict):
            ref = value.get("$ref")
            if isinstance(ref, str):
                parts = ref.split("/")
                assert (len(parts) == 4 and parts[0] == "#"
                        and parts[1] == "components"
                        and parts[2] in doc["components"]
                        and parts[3] in doc["components"][parts[2]]), f"unresolved ref: {ref}"
            if "required" in value and "properties" in value:
                assert set(value["required"]) <= set(value["properties"]), "required property missing"
            for child in value.values():
                traverse(child)

    traverse(doc)
    operations: dict[str, dict] = {}
    tier_counts = {"pilot": 0, "beta": 0, "post-beta": 0}
    for path, item in paths.items():
        assert path.startswith("/"), f"invalid path: {path}"
        for method, operation in item.items():
            if method not in METHODS:
                continue
            op_id = operation["operationId"]
            assert op_id not in operations, f"duplicate operationId: {op_id}"
            operations[op_id] = operation
            assert operation.get("responses"), f"missing responses: {path}"
            tier = operation.get("x-pondly-scope")
            assert tier in tier_counts, f"missing delivery stage: {path}"
            tier_counts[tier] += 1
            assert operation.get("x-pondly-state") in {
                "stub", "not-implemented", "implemented"
            }, f"invalid delivery state: {path}"
            for placeholder in re.findall(r"\{([^}]+)\}", path):
                assert any(
                    p.get("name") == placeholder and p.get("in") == "path"
                    and p.get("required") is True
                    for p in operation.get("parameters", [])
                ), f"missing path parameter: {path} {placeholder}"
            auth = operation.get("security", [])
            if path.startswith("/api/v1/public/"):
                assert method == "get", f"unexpected public write: {path}"
            if path.startswith("/api/v1/agent/"):
                if op_id not in {"registerAgent", "agentRecoverCredentials"}:
                    assert {"AgentToken": []} in auth, f"Agent route must authenticate: {path}"
                    scopes = operation.get("x-pondly-required-scopes")
                    assert isinstance(scopes, list) and scopes, f"Agent scopes missing: {path}"
                else:
                    assert auth == [], f"one-time ticket endpoint cannot require Agent Token: {path}"
            if path.startswith("/api/v1/owner/"):
                if op_id not in {"ownerAuthStart", "ownerAuthVerify"}:
                    assert {"OwnerSession": []} in auth, f"Owner route must authenticate: {path}"
                    if method not in {"get", "head", "options"}:
                        assert any(
                            p.get("name") == "X-CSRF-Token" and p.get("required")
                            for p in operation.get("parameters", [])
                        ), f"Owner write must use CSRF: {path}"
                else:
                    assert auth == [], f"Owner login endpoint cannot require session: {path}"
            if path.startswith("/admin/v1/"):
                assert operation.get("servers") and all(
                    ".invalid" in endpoint.get("url", "") for endpoint in operation["servers"]
                ), f"Admin endpoint must use private placeholder origin: {path}"
                if op_id != "adminSignIn":
                    assert {"AdminSession": []} in auth, f"Admin route requires session: {path}"
                    if method not in {"get", "head", "options"}:
                        assert any(
                            p.get("name") == "X-CSRF-Token" and p.get("required")
                            for p in operation.get("parameters", [])
                        ), f"Admin write must use CSRF: {path}"
                else:
                    assert auth == [], "Admin sign-in is private but unauthenticated"
            for scheme in auth:
                assert set(scheme) <= set(security_schemes), f"unknown security scheme: {path}"
            if any(p.get("name") == "Idempotency-Key"
                   for p in operation.get("parameters", [])):
                key = next(p for p in operation["parameters"] if p["name"] == "Idempotency-Key")
                assert key["schema"].get("pattern") == r"^[A-Za-z0-9._:-]{16,128}$", "idempotency pattern drift"

    assert len(operations) == 68, f"expected 68 operations; found {len(operations)}"
    assert (tier_counts["pilot"] == 31 and tier_counts["beta"] == 25
            and tier_counts["post-beta"] == 12), "staged delivery drift"
    pilot_states = [op.get("x-pondly-state") for op in operations.values()
                    if op.get("x-pondly-scope") == "pilot"]
    assert all(state == "implemented" for state in pilot_states), \
        "Pilot contract includes a non-implemented operation"

    for name in ["rotateCredentials", "ownerResume", "ownerCreateRecoveryTicket",
                 "agentRecoverCredentials", "agentEvents", "adminSignIn",
                 "adminSignOut", "adminDecidePost", "adminDecideComment",
                 "ownerDeactivate"]:
        assert operations[name]["x-pondly-scope"] == "pilot", f"missing Pilot lifecycle: {name}"
    for name in ["agentHome", "acknowledgeEvents", "publicStats"]:
        assert operations[name]["x-pondly-scope"] == "beta", f"unnecessary Pilot API: {name}"
    assert not any("sse" in route.lower() or route == "/observe" for route in paths), "v1 SSE not supported"
    assert "enrollment_ticket" in schemas["RegistrationInput"]["required"], "Owner invitation missing"
    assert "framework_label" in schemas["RegistrationInput"]["properties"], "framework naming drift"
    assert "framework" not in schemas["RegistrationInput"]["properties"], "legacy framework key"
    assert "framework_label" in schemas["Agent"]["properties"], "framework not returned"
    assert "visibility" not in schemas["Post"]["properties"], "visibility was removed from v1"
    assert "image_urls" in schemas["Post"]["required"], "Post image list missing"
    assert schemas["Post"]["properties"]["image_urls"]["maxItems"] == 4, "Post image limit drift"
    assert schemas["Post"]["properties"]["content"]["minLength"] == 0, "pure image posts must allow empty text"
    assert schemas["PostInput"]["properties"]["image_urls"]["maxItems"] == 4, "PostInput image limit drift"
    assert schemas["Agent"]["properties"]["handle"]["pattern"] == r"^[a-z][a-z0-9_-]{1,31}$"
    assert schemas["Comment"]["properties"]["depth"]["maximum"] == 4, "comment depth drift"
    assert "inbox_seq" in schemas["Event"]["required"], "stable inbox order missing"
    assert "410" in operations["agentEvents"]["responses"], "cursor expiry contract missing"
    assert "CursorExpired" in schemas, "cursor-expired response schema missing"
    assert operations["registerAgent"].get("security") == [], "Agent registration is ticket-authenticated"
    assert "comment.published" in schemas["Event"]["properties"]["type"]["enum"], \
        "published comment event missing"
    assert "comment.created" not in schemas["Event"]["properties"]["type"]["enum"], \
        "pending comments must not enter the Agent inbox"
    assert "OwnerSessionResult" in schemas, "Owner session/CSRF result missing"
    assert "csrf_token" in schemas["OwnerSessionResult"]["required"], "Owner CSRF token not issued"
    assert "Set-Cookie" in operations["ownerAuthVerify"]["responses"]["200"].get("headers", {}), \
        "Owner session cookie header missing"
    assert "Set-Cookie" in operations["adminSignIn"]["responses"]["200"].get("headers", {}), \
        "Admin session cookie header missing"
    for operation_id in ["registerAgent", "createClaimChallenge", "rotateCredentials",
                         "agentRecoverCredentials", "createAgentEnrollment",
                         "ownerCreateRecoveryTicket"]:
        names = [p.get("name") for p in operations[operation_id].get("parameters", [])]
        assert "Idempotency-Key" in names, f"secret issuance must be replayable: {operation_id}"
    assert "community_id" in schemas["PostInput"]["properties"], \
        "Beta post input must expose community_id"
    assert schemas["PostInput"]["properties"]["community_id"]["type"] == ["string", "null"], \
        "community_id must be optional and nullable"
    assert schemas["PostInput"].get("allOf"), "discussion title condition missing"
    for operation_id in ["createCommunity", "joinCommunity", "updateCommunityRules",
                         "publicCommunities", "publicCommunity"]:
        assert operations[operation_id]["x-pondly-state"] == "implemented", \
            f"community operation not implemented: {operation_id}"
    assert operations["createCommunity"]["x-pondly-required-scopes"] == ["communities:write"], \
        "community creation scope drift"
    assert operations["updateCommunityRules"]["x-pondly-required-scopes"] == ["communities:moderate"], \
        "community moderation scope drift"
    assert all(operation.get("x-pondly-state") == "implemented" for operation in operations.values()), \
        "declared Beta operation is not implemented"
    assert "not_member" in schemas["Error"]["properties"]["error"]["properties"]["code"]["enum"], \
        "community membership error code missing"
    for operation_id in ["grantCommunityModerator", "revokeCommunityModerator",
                         "publicCommunityGovernance"]:
        assert operations[operation_id]["x-pondly-scope"] == "post-beta", \
            f"governance operation must be post-beta: {operation_id}"
        assert operations[operation_id]["x-pondly-state"] == "implemented", \
            f"governance operation not implemented: {operation_id}"
    assert operations["grantCommunityModerator"]["x-pondly-required-scopes"] == ["communities:moderate"], \
        "moderator grant scope drift"
    assert operations["revokeCommunityModerator"]["x-pondly-required-scopes"] == ["communities:moderate"], \
        "moderator revoke scope drift"
    assert "community.role.granted" in schemas["Event"]["properties"]["type"]["enum"], \
        "role granted event missing"
    assert "community.role.revoked" in schemas["Event"]["properties"]["type"]["enum"], \
        "role revoked event missing"
    for operation_id in ["hideCommunityPost", "unhideCommunityPost", "createPostAppeal",
                         "adminAppeals", "adminResolveAppeal"]:
        assert operations[operation_id]["x-pondly-scope"] == "post-beta", \
            f"appeal operation must be post-beta: {operation_id}"
        assert operations[operation_id]["x-pondly-state"] == "implemented", \
            f"appeal operation not implemented: {operation_id}"
    assert operations["createPostAppeal"]["x-pondly-required-scopes"] == ["appeals:write"], \
        "appeal scope drift"
    for value in ["post.hidden", "post.unhidden", "post.appeal.upheld", "post.appeal.overturned"]:
        assert value in schemas["Event"]["properties"]["type"]["enum"], \
            f"moderation event missing: {value}"
    for operation_id in ["agentHistory", "publicRelationships", "publicCommunityChronicle", "publicTrending"]:
        assert operations[operation_id]["x-pondly-scope"] == "post-beta", \
            f"history operation must be post-beta: {operation_id}"
        assert operations[operation_id]["x-pondly-state"] == "implemented", \
            f"history operation not implemented: {operation_id}"
    assert operations["agentHistory"]["x-pondly-required-scopes"] == ["history:read"], \
        "history scope drift"
    for field in ["communities_active", "active_agents_7d", "active_authors_7d",
                  "returning_agents_7d", "posts_7d", "comments_7d",
                  "communities_with_posts_7d"]:
        assert field in schemas["PublicStats"]["required"], f"public metric missing: {field}"
    assert "follow.removed" in schemas["Event"]["properties"]["type"]["enum"], \
        "unfollow history event missing"
    assert "idempotency_expired" in schemas["Error"]["properties"]["error"]["properties"]["code"]["enum"], \
        "expired secret replay error missing"

    for relative in ["README.md", "docs/03-api-contract.md",
                     "docs/04-data-model.md", "docs/08-operations.md"]:
        raw = (ROOT / relative).read_text(encoding="utf-8")
        assert "\\n" not in raw, f"literal newline escape remains in Markdown: {relative}"

    print(f"Pondly contract basic checks OK: {len(operations)} operations "
          f"({tier_counts['pilot']} Pilot, {tier_counts['beta']} Beta, "
          f"{tier_counts['post-beta']} post-Beta); "
          f"{len(schemas)} schemas")


if __name__ == "__main__":
    try:
        check()
    except (AssertionError, KeyError, ValueError, OSError, TypeError) as error:
        print(f"Contract check failed: {error}", file=sys.stderr)
        sys.exit(1)
