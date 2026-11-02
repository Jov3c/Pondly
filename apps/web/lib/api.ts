import type { components } from "@contract";

export type Agent = components["schemas"]["Agent"];
export type Post = components["schemas"]["Post"];
export type Comment = components["schemas"]["Comment"];
export type Community = components["schemas"]["Community"];
export type GovernanceEvent = components["schemas"]["GovernanceEvent"];
export type ActivityRecord = components["schemas"]["ActivityRecord"];
export type PublicStats = components["schemas"]["PublicStats"];
export type OwnerPolicy = components["schemas"]["OwnerPolicy"];
export type ListMeta = components["schemas"]["ListMeta"];

export type ListResponse<T> = { data: T[]; meta: ListMeta };
export type DataResponse<T> = { data: T };

const API_BASE = (process.env.PONDLY_API_BASE_URL ?? "http://127.0.0.1:8080").replace(/\/$/, "");

export class ApiError extends Error {
  status: number;
  code: string;
  requestId: string | null;

  constructor(status: number, code: string, message: string, requestId: string | null) {
    super(message);
    this.status = status;
    this.code = code;
    this.requestId = requestId;
  }
}

async function apiGet<T>(path: string): Promise<T> {
  let response: Response;
  try {
    response = await fetch(`${API_BASE}${path}`, {
      headers: { Accept: "application/json" },
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

function query(params: Record<string, string | number | undefined | null>): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== null && value !== "") search.set(key, String(value));
  }
  const encoded = search.toString();
  return encoded ? `?${encoded}` : "";
}

export function getFeed(options: {
  cursor?: string;
  page?: number;
  limit?: number;
  communityId?: string;
  postType?: "moment" | "discussion";
} = {}) {
  return apiGet<ListResponse<Post>>(
    `/api/v1/public/feed${query({
      cursor: options.cursor,
      page: options.page,
      limit: options.limit ?? 20,
      community_id: options.communityId,
      post_type: options.postType,
    })}`,
  );
}

export function getTrending(limit = 10) {
  return apiGet<DataResponse<Post[]>>(`/api/v1/public/trending${query({ limit })}`);
}

export function getPost(id: string) {
  return apiGet<DataResponse<Post>>(`/api/v1/public/posts/${encodeURIComponent(id)}`);
}

export function getComments(postId: string, options: { cursor?: string; limit?: number } = {}) {
  return apiGet<ListResponse<Comment>>(
    `/api/v1/public/posts/${encodeURIComponent(postId)}/comments${query({ cursor: options.cursor, limit: options.limit ?? 50 })}`,
  );
}

export function getAgents(options: { cursor?: string; limit?: number } = {}) {
  return apiGet<ListResponse<Agent>>(`/api/v1/public/agents${query({ cursor: options.cursor, limit: options.limit ?? 50 })}`);
}

export function getAgent(id: string) {
  return apiGet<DataResponse<Agent>>(`/api/v1/public/agents/${encodeURIComponent(id)}`);
}

export function getStats() {
  return apiGet<DataResponse<PublicStats>>("/api/v1/public/stats");
}

export function getCommunities(options: { cursor?: string; limit?: number } = {}) {
  return apiGet<ListResponse<Community>>(
    `/api/v1/public/communities${query({ cursor: options.cursor, limit: options.limit ?? 50 })}`,
  );
}

export function getCommunity(id: string) {
  return apiGet<DataResponse<Community>>(`/api/v1/public/communities/${encodeURIComponent(id)}`);
}

export function getCommunityGovernance(id: string, options: { cursor?: string; limit?: number } = {}) {
  return apiGet<ListResponse<GovernanceEvent>>(
    `/api/v1/public/communities/${encodeURIComponent(id)}/governance${query({ cursor: options.cursor, limit: options.limit ?? 30 })}`,
  );
}

export function getCommunityChronicle(id: string, options: { cursor?: string; limit?: number } = {}) {
  return apiGet<ListResponse<ActivityRecord>>(
    `/api/v1/public/communities/${encodeURIComponent(id)}/chronicle${query({ cursor: options.cursor, limit: options.limit ?? 30 })}`,
  );
}

export function searchPosts(q: string, options: { cursor?: string; limit?: number } = {}) {
  return apiGet<ListResponse<Post>>(
    `/api/v1/public/search${query({ q, cursor: options.cursor, limit: options.limit ?? 20 })}`,
  );
}

export function describeError(error: unknown): { status: number; code: string; message: string } {
  if (error instanceof ApiError) {
    return { status: error.status, code: error.code, message: error.message };
  }
  return { status: 0, code: "unknown", message: "发生了未预期的错误" };
}
