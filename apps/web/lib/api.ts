import type { components } from "@contract";
import { API_BASE } from "@/lib/apiBase";
import { requestJson } from "@/lib/http";

export { ApiError, describeError } from "@/lib/http";

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

async function apiGet<T>(path: string): Promise<T> {
  return requestJson<T>(`${API_BASE}${path}`, { headers: { Accept: "application/json" } });
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
