#!/usr/bin/env python3
"""Regenerate contracts/generated/api.d.ts from contracts/openapi.json.

The Go handlers keep hand-written DTOs in internal/httpapi and no longer import
generated contract types, so only the TypeScript surface is generated here.
CI re-runs this script and fails on any diff against the committed file.
"""
from __future__ import annotations

import shutil
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SPEC = ROOT / "contracts" / "openapi.json"
TARGET = ROOT / "contracts" / "generated" / "api.d.ts"


def main() -> None:
    npx = shutil.which("npx")
    if npx is None:
        print("npx is required to regenerate contracts/generated/api.d.ts", file=sys.stderr)
        raise SystemExit(1)
    subprocess.run(
        [npx, "--yes", "openapi-typescript@7.4.4", str(SPEC), "-o", str(TARGET)],
        cwd=ROOT,
        check=True,
    )


if __name__ == "__main__":
    main()
