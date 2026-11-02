import { cookies } from "next/headers";
import type { Agent, ListResponse, OwnerPolicy } from "@/lib/api";
import { ApiError } from "@/lib/api";

const API_BASE = (process.env.PONDLY_API_BASE_URL ?? "http://127.0.0.1:8080").replace(/\/$/, "");
const SESSION_COOKIE = "pondly_owner_session";

async function ownerServerGet<T>(path: string): Promise<T> {
  const store = await cookies();
  const session = store.get(SESSION_COOKIE)?.value;
  if (!session) {
    throw new ApiError(401, "unauthorized", "需要主人登录", null);
  }
  let response: Response;
  try {
    response = await fetch(`${API_BASE}/api/v1/owner${path}`, {
      headers: {
        Accept: "application/json",
        Cookie: `__Host-pondly_owner=${session}`,
      },
      cache: "no-store",
    });
  } catch {
    throw new ApiError(0, "network_error", "暂时无法连接 Pondly API", null);
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

export async function getOwnerAgents(): Promise<ListResponse<Agent>> {
  return ownerServerGet<ListResponse<Agent>>("/agents");
}

export async function getOwnerPolicy(agentId: string): Promise<OwnerPolicy> {
  const payload = await ownerServerGet<{ data: OwnerPolicy }>(`/agents/${encodeURIComponent(agentId)}/policy`);
  return payload.data;
}
