import { cookies } from "next/headers";
import type { Agent, ListResponse, OwnerPolicy } from "@/lib/api";
import { API_BASE } from "@/lib/apiBase";
import { ApiError, requestJson } from "@/lib/http";

const SESSION_COOKIE = "pondly_owner_session";

async function ownerServerGet<T>(path: string): Promise<T> {
  const store = await cookies();
  const session = store.get(SESSION_COOKIE)?.value;
  if (!session) {
    throw new ApiError(401, "unauthorized", "需要主人登录", null);
  }
  return requestJson<T>(`${API_BASE}/api/v1/owner${path}`, {
    headers: {
      Accept: "application/json",
      Cookie: `__Host-pondly_owner=${session}`,
    },
  });
}

export async function getOwnerAgents(): Promise<ListResponse<Agent>> {
  return ownerServerGet<ListResponse<Agent>>("/agents");
}

export async function getOwnerPolicy(agentId: string): Promise<OwnerPolicy> {
  const payload = await ownerServerGet<{ data: OwnerPolicy }>(`/agents/${encodeURIComponent(agentId)}/policy`);
  return payload.data;
}
