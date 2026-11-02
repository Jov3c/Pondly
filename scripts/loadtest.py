#!/usr/bin/env python3
"""Read-only public load probe for isolated staging.

Only GET requests to public endpoints. Refuses non-loopback targets unless
--allow-remote is explicit. Does not authenticate, write, or touch admin paths.
"""

from __future__ import annotations

import argparse
import concurrent.futures
import json
import statistics
import time
import urllib.error
import urllib.parse
import urllib.request

ENDPOINTS = ["/api/v1/public/feed", "/api/v1/public/stats",
             "/api/v1/public/communities", "/api/v1/public/agents"]


def percentile(values: list[float], p: float) -> float:
    if not values:
        return 0.0
    index = min(len(values) - 1, int(len(values) * p))
    return values[index]


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", required=True)
    parser.add_argument("--seconds", type=float, default=30.0)
    parser.add_argument("--concurrency", type=int, default=10)
    parser.add_argument("--allow-remote", action="store_true")
    args = parser.parse_args()

    parsed = urllib.parse.urlsplit(args.base_url)
    if parsed.scheme not in {"http", "https"} or not parsed.hostname:
        raise SystemExit("base-url must be http(s)")
    if not args.allow_remote and parsed.hostname not in {"127.0.0.1", "localhost", "::1"}:
        raise SystemExit("refusing non-loopback target; pass --allow-remote for isolated staging")

    deadline = time.monotonic() + max(1.0, args.seconds)
    latencies: list[float] = []
    statuses: dict[int, int] = {}

    def worker() -> None:
        while time.monotonic() < deadline:
            for endpoint in ENDPOINTS:
                url = args.base_url.rstrip("/") + endpoint
                started = time.monotonic()
                try:
                    with urllib.request.urlopen(url, timeout=10) as response:
                        response.read(1024)
                        status = response.status
                except urllib.error.HTTPError as error:
                    status = error.code
                except OSError:
                    status = 0
                latencies.append((time.monotonic() - started) * 1000)
                statuses[status] = statuses.get(status, 0) + 1

    with concurrent.futures.ThreadPoolExecutor(max_workers=max(1, args.concurrency)) as pool:
        futures = [pool.submit(worker) for _ in range(max(1, args.concurrency))]
        for future in futures:
            future.result()

    latencies.sort()
    print(json.dumps({
        "base_url": args.base_url,
        "requests": len(latencies),
        "statuses": statuses,
        "p50_ms": round(percentile(latencies, 0.5), 2),
        "p95_ms": round(percentile(latencies, 0.95), 2),
        "p99_ms": round(percentile(latencies, 0.99), 2),
        "mean_ms": round(statistics.fmean(latencies), 2) if latencies else 0,
    }, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
