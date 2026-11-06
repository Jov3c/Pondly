import { cookies } from "next/headers";
import { NextRequest, NextResponse } from "next/server";

const API_BASE = (process.env.PONDLY_API_BASE_URL ?? "http://127.0.0.1:8080").replace(/\/$/, "");
const SESSION_COOKIE = "pondly_owner_session";
const CSRF_COOKIE = "pondly_owner_csrf";
const WRITE_METHODS = new Set(["POST", "PATCH", "PUT", "DELETE"]);

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type RouteContext = { params: Promise<{ path: string[] }> };

function cookieOptions(maxAge?: number, httpOnly = false) {
  return {
    httpOnly,
    secure: process.env.NODE_ENV === "production",
    sameSite: "lax" as const,
    path: "/",
    ...(maxAge ? { maxAge } : {}),
  };
}

function clearAuthCookies(response: NextResponse) {
  response.cookies.set(SESSION_COOKIE, "", { ...cookieOptions(0, true), maxAge: 0 });
  response.cookies.set(CSRF_COOKIE, "", { ...cookieOptions(0, false), maxAge: 0 });
}

function upstreamCookieToken(response: Response): string | null {
  const getSetCookie = (response.headers as Headers & { getSetCookie?: () => string[] }).getSetCookie;
  const cookies = typeof getSetCookie === "function"
    ? getSetCookie.call(response.headers)
    : [response.headers.get("set-cookie") ?? ""];
  for (const cookie of cookies) {
    const first = cookie.split(";")[0] ?? "";
    if (first.startsWith("__Host-pondly_owner=")) {
      return first.slice("__Host-pondly_owner=".length);
    }
  }
  return null;
}

async function handle(request: NextRequest, context: RouteContext) {
  const { path } = await context.params;
  if (path.some((segment) => !/^[A-Za-z0-9_-]+$/.test(segment))) {
    return NextResponse.json(
      { error: { code: "invalid_input", message: "Invalid owner path", request_id: "" } },
      { status: 400 },
    );
  }
  const subpath = path.join("/");
  const isAuth = subpath === "auth/start" || subpath === "auth/verify";
  const store = await cookies();
  const session = store.get(SESSION_COOKIE)?.value;
  const csrfCookie = store.get(CSRF_COOKIE)?.value ?? "";

  if (!isAuth && !session) {
    return NextResponse.json(
      { error: { code: "unauthorized", message: "Owner session required", request_id: "" } },
      { status: 401 },
    );
  }
  if (WRITE_METHODS.has(request.method) && !isAuth) {
    const csrfHeader = request.headers.get("x-csrf-token") ?? "";
    if (!csrfHeader || !csrfCookie || csrfHeader !== csrfCookie) {
      return NextResponse.json(
        { error: { code: "insufficient_scope", message: "CSRF token required", request_id: "" } },
        { status: 403 },
      );
    }
  }

  const body = request.method === "GET" ? undefined : await request.text();
  const headers = new Headers({ Accept: "application/json" });
  // Without the real client address the API would collapse every owner into one IP bucket.
  const forwardedFor = request.headers.get("x-forwarded-for");
  if (forwardedFor) headers.set("X-Forwarded-For", forwardedFor);
  if (body) headers.set("Content-Type", request.headers.get("content-type") ?? "application/json");
  if (session) headers.set("Cookie", `__Host-pondly_owner=${session}`);
  const csrfHeader = request.headers.get("x-csrf-token");
  if (csrfHeader) headers.set("X-CSRF-Token", csrfHeader);
  const idempotencyKey = request.headers.get("idempotency-key");
  if (idempotencyKey) headers.set("Idempotency-Key", idempotencyKey);

  let upstream: Response;
  try {
    upstream = await fetch(`${API_BASE}/api/v1/owner/${subpath}${request.nextUrl.search}`, {
      method: request.method,
      headers,
      body: body || undefined,
      cache: "no-store",
    });
  } catch {
    return NextResponse.json(
      { error: { code: "network_error", message: "Pondly API 不可用", request_id: "" } },
      { status: 503 },
    );
  }

  const text = await upstream.text();
  let payload: unknown = null;
  try {
    payload = text ? JSON.parse(text) : null;
  } catch {
    payload = null;
  }
  if (!text) {
    const empty = new NextResponse(null, { status: upstream.status });
    empty.headers.set("Cache-Control", "no-store");
    if (upstream.status === 401) clearAuthCookies(empty);
    return empty;
  }

  const response = NextResponse.json(payload, { status: upstream.status });
  response.headers.set("Cache-Control", "no-store");

  if (subpath === "auth/verify" && upstream.ok) {
    const data = (payload as { data?: { csrf_token?: string; expires_at?: string } } | null)?.data;
    const token = upstreamCookieToken(upstream);
    const csrf = data?.csrf_token;
    let maxAge: number | undefined;
    if (data?.expires_at) {
      const seconds = Math.floor((new Date(data.expires_at).getTime() - Date.now()) / 1000);
      if (Number.isFinite(seconds) && seconds > 0) maxAge = seconds;
    }
    if (token) response.cookies.set(SESSION_COOKIE, token, cookieOptions(maxAge, true));
    if (csrf) response.cookies.set(CSRF_COOKIE, csrf, cookieOptions(maxAge, false));
  }
  if (subpath === "auth/sign-out" && upstream.ok) clearAuthCookies(response);
  if (upstream.status === 401 && !isAuth) clearAuthCookies(response);
  return response;
}

export async function GET(request: NextRequest, context: RouteContext) {
  return handle(request, context);
}

export async function POST(request: NextRequest, context: RouteContext) {
  return handle(request, context);
}

export async function PATCH(request: NextRequest, context: RouteContext) {
  return handle(request, context);
}

export async function DELETE(request: NextRequest, context: RouteContext) {
  return handle(request, context);
}
