import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { test } from "node:test";

const moduleURL = new URL("./apiBase.ts", import.meta.url).href;

// Each case needs a fresh module evaluation, because the bases are read once at
// import time. A child process is the only way to vary process.env per case.
function readBases(env: Record<string, string>): { API_BASE: string; PUBLIC_API_BASE: string } {
  const childEnv: NodeJS.ProcessEnv = { ...process.env, ...env };
  for (const key of ["PONDLY_API_BASE_URL", "PONDLY_PUBLIC_API_BASE_URL"]) {
    if (!(key in env)) delete childEnv[key];
  }
  const script = `const m = await import(${JSON.stringify(moduleURL)});
process.stdout.write(JSON.stringify({ API_BASE: m.API_BASE, PUBLIC_API_BASE: m.PUBLIC_API_BASE }));`;
  const output = execFileSync(process.execPath, ["--experimental-strip-types", "--input-type=module", "-e", script], {
    env: childEnv,
    encoding: "utf8",
  });
  return JSON.parse(output);
}

test("both bases fall back to the local API when nothing is configured", () => {
  const bases = readBases({});
  assert.equal(bases.API_BASE, "http://127.0.0.1:8080");
  assert.equal(bases.PUBLIC_API_BASE, "http://127.0.0.1:8080");
});

test("a configured API base loses its trailing slash and is reused for the public base", () => {
  const bases = readBases({ PONDLY_API_BASE_URL: "http://127.0.0.1:9000/" });
  assert.equal(bases.API_BASE, "http://127.0.0.1:9000");
  assert.equal(bases.PUBLIC_API_BASE, "http://127.0.0.1:9000");
});

test("the public base can point at the edge without moving server-side calls", () => {
  const bases = readBases({
    PONDLY_API_BASE_URL: "http://127.0.0.1:9000",
    PONDLY_PUBLIC_API_BASE_URL: "https://pondly.example.com/",
  });
  assert.equal(bases.API_BASE, "http://127.0.0.1:9000");
  assert.equal(bases.PUBLIC_API_BASE, "https://pondly.example.com");
});

test("an empty public base falls back instead of emptying the Agent prompt", () => {
  const bases = readBases({
    PONDLY_API_BASE_URL: "http://127.0.0.1:9000",
    PONDLY_PUBLIC_API_BASE_URL: "",
  });
  assert.equal(bases.PUBLIC_API_BASE, "http://127.0.0.1:9000");
});

test("an empty API base falls back to the local API", () => {
  const bases = readBases({ PONDLY_API_BASE_URL: "" });
  assert.equal(bases.API_BASE, "http://127.0.0.1:8080");
  assert.equal(bases.PUBLIC_API_BASE, "http://127.0.0.1:8080");
});