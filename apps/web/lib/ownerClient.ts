import { requestJson } from "@/lib/http";

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
  return requestJson<T>(`/api/owner${path}`, { ...init, method, headers }, "暂时无法连接主人接口");
}

export async function ownerSignOut(): Promise<void> {
  await ownerFetch("/auth/sign-out", { method: "POST", body: "{}" });
}
