import { ApiError } from "@/lib/api";

function readCookie(name: string): string {
  if (typeof document === "undefined") return "";
  const prefix = `${name}=`;
  const row = document.cookie.split("; ").find((entry) => entry.startsWith(prefix));
  return row ? decodeURIComponent(row.slice(prefix.length)) : "";
}

export async function ownerFetch<T = unknown>(path: string, init: RequestInit = {}): Promise<T> {
  const method = (init.method ?? "GET").toUpperCase();
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  if (init.body) headers.set("Content-Type", "application/json");
  if (method !== "GET") {
    const csrf = readCookie("pondly_owner_csrf");
    if (csrf) headers.set("X-CSRF-Token", csrf);
    if (!headers.has("Idempotency-Key")) {
      headers.set("Idempotency-Key", crypto.randomUUID());
    }
  }
  let response: Response;
  try {
    response = await fetch(`/api/owner${path}`, {
      ...init,
      method,
      headers,
      cache: "no-store",
    });
  } catch {
    throw new ApiError(0, "network_error", "暂时无法连接主人接口", null);
  }
  const text = await response.text();
  let payload: unknown = null;
  try {
    payload = text ? JSON.parse(text) : null;
  } catch {
    payload = null;
  }
  if (!response.ok) {
    const error = (payload as { error?: { code?: string; message?: string; request_id?: string } } | null)?.error;
    throw new ApiError(
      response.status,
      error?.code ?? `http_${response.status}`,
      error?.message ?? "请求失败",
      error?.request_id ?? null,
    );
  }
  return payload as T;
}

export async function ownerSignOut(): Promise<void> {
  await ownerFetch("/auth/sign-out", { method: "POST", body: "{}" });
}
