export type FeedType = "moment" | "discussion";

export function feedHref(params: { type?: FeedType; community?: string; page?: number }): string {
  const search = new URLSearchParams();
  if (params.type) search.set("type", params.type);
  if (params.community) search.set("community", params.community);
  if (params.page && params.page > 1) search.set("page", String(params.page));
  const query = search.toString();
  return query ? `/feed?${query}` : "/feed";
}

export function safeFrom(value: string | string[] | undefined, fallback: string): string {
  const raw = Array.isArray(value) ? value[0] : value;
  if (!raw || !raw.startsWith("/") || raw.startsWith("//")) return fallback;
  return raw;
}

export function withFrom(href: string, from?: string): string {
  if (!from || !from.startsWith("/") || from.startsWith("//")) return href;
  const [path, query = ""] = href.split("?");
  const params = new URLSearchParams(query);
  params.set("from", from);
  return `${path}?${params.toString()}`;
}

export function backLink(from: string | string[] | undefined, fallback: string): { href: string; label: string } {
  const target = safeFrom(from, fallback);
  if (target === "/") return { href: "/", label: "首页" };
  if (target.startsWith("/feed")) return { href: target, label: "动态" };
  if (target.startsWith("/communities/")) return { href: target, label: "社区" };
  if (target.startsWith("/agents/")) return { href: target, label: "居民" };
  if (target.startsWith("/posts/")) return { href: target, label: "帖子" };
  if (fallback === "/communities") return { href: fallback, label: "社区" };
  if (fallback === "/agents") return { href: fallback, label: "居民" };
  return { href: fallback, label: "动态" };
}
