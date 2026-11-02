#!/usr/bin/env python3
"""Regenerate Go and TypeScript contract types.

oapi-codegen 2.4 does not accept OpenAPI 3.1 nullable type arrays, so the Go
types are generated from a temporary 3.0-compatible copy. The canonical
contract remains contracts/openapi.json (3.1) and is the only source of truth.
"""
from __future__ import annotations

import json
import shutil
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SPEC = ROOT / "contracts" / "openapi.json"
TMP_SPEC = ROOT / "contracts" / ".openapi.v30.tmp.json"


def to_openapi30(doc: dict) -> dict:
    def convert(value: object) -> None:
        if isinstance(value, dict):
            kind = value.get("type")
            if isinstance(kind, list) and "null" in kind:
                rest = [item for item in kind if item != "null"]
                if len(rest) == 1:
                    value["type"] = rest[0]
                    value["nullable"] = True
            if "const" in value:
                value["enum"] = [value.pop("const")]
            for child in list(value.values()):
                convert(child)
        elif isinstance(value, list):
            for child in value:
                convert(child)

    convert(doc)
    doc["openapi"] = "3.0.3"
    return doc


def run(command: list[str], cwd: Path) -> None:
    subprocess.run(command, cwd=cwd, check=True)


def main() -> None:
    doc = json.loads(SPEC.read_text(encoding="utf-8"))
    TMP_SPEC.write_text(json.dumps(to_openapi30(doc), indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    try:
        run([
            "go", "run", "github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.1",
            "-generate", "types", "-package", "contract",
            "-o", "internal/contract/types.gen.go",
            "../../contracts/.openapi.v30.tmp.json",
        ], ROOT / "apps" / "api")
    finally:
        TMP_SPEC.unlink(missing_ok=True)

    npx = shutil.which("npx")
    if npx is None:
        print("npx not found; skipped TypeScript generation", file=sys.stderr)
        return
    run([npx, "--yes", "openapi-typescript@7.4.4", "contracts/openapi.json",
         "-o", "contracts/generated/api.d.ts"], ROOT)


if __name__ == "__main__":
    main()
