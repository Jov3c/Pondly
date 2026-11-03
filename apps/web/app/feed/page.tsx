import type { Metadata } from "next";
import Link from "next/link";
import { FeedPagination } from "@/components/FeedPagination";
import { EmptyState, ErrorPanel } from "@/components/Notice";
import { PostRow } from "@/components/PostRow";
import { getCommunities, getFeed, getTrending } from "@/lib/api";
import { excerpt } from "@/lib/format";
import { withFrom } from "@/lib/navigation";

export const dynamic = "force-dynamic";

export const metadata: Metadata = { title: "动态" };

const categories = [
  { label: "全部", type: undefined },
  { label: "动态", type: "moment" as const },
  { label: "讨论", type: "discussion" as const },
];

type FeedType = "moment" | "discussion";

function feedHref(params: { type?: FeedType; community?: string; page?: number }) {
  const search = new URLSearchParams();
  if (params.type) search.set("type", params.type);
  if (params.community) search.set("community", params.community);
  if (params.page && params.page > 1) search.set("page", String(params.page));
  const query = search.toString();
  return query ? `/feed?${query}` : "/feed";
}

export default async function FeedPage({
  searchParams,
}: {
  searchParams: Promise<{ page?: string; type?: string; community?: string }>;
}) {
  const { page, type, community } = await searchParams;
  const activeType = type === "moment" || type === "discussion" ? type : undefined;
  const activeCommunityId = typeof community === "string" && community ? community : undefined;
  const requestedPage = Number.parseInt(page ?? "1", 10);
  const currentPage = Number.isFinite(requestedPage) && requestedPage > 0 ? requestedPage : 1;

  const [feedResult, communitiesResult, trendingResult] = await Promise.allSettled([
    getFeed({
      page: currentPage,
      limit: 5,
      postType: activeType,
      communityId: activeCommunityId,
    }),
    getCommunities({ limit: 50 }),
    getTrending(6),
  ]);
  const feed = feedResult.status === "fulfilled" ? feedResult.value : null;
  const communities = communitiesResult.status === "fulfilled" ? communitiesResult.value.data : [];
  const trending = trendingResult.status === "fulfilled" ? trendingResult.value.data : [];
  const communityNames = new Map(communities.map((item) => [item.id, item.name]));
  const activeCommunity = communities.find((item) => item.id === activeCommunityId);
  const activeLabel = activeCommunity?.name ?? categories.find((item) => item.type === activeType)?.label ?? "全部";
  const feedPage = feed?.meta.page ?? currentPage;
  const hasMore = feed?.meta.has_more ?? false;
  const feedPath = feedHref({
    type: activeType,
    community: activeCommunityId,
    page: currentPage,
  });

  return (
    <main className="feed-page home-page">
      <h1 className="sr-only">公开动态</h1>

      <div className="home-layout">
        <aside className="home-categories" aria-label="分类">
          <div className="side-head">
            <span className="side-title">分类</span>
          </div>
          <nav className="category-list">
            {categories.map((item) => (
              <Link
                key={item.label}
                href={feedHref({ type: item.type, community: activeCommunityId })}
                scroll={false}
                className={activeType === item.type ? "category-link is-active" : "category-link"}
                aria-current={activeType === item.type ? "page" : undefined}
              >
                {item.label}
              </Link>
            ))}
          </nav>
          <div className="side-head side-gap">
            <span className="side-title">社区</span>
          </div>
          <nav className="community-links" aria-label="按社区筛选">
            <Link
              href={feedHref({ type: activeType })}
              scroll={false}
              className={!activeCommunityId ? "community-link is-active" : "community-link"}
              aria-current={!activeCommunityId ? "page" : undefined}
            >
              全部社区
            </Link>
            {communities.slice(0, 10).map((item) => (
              <Link
                key={item.id}
                href={feedHref({ type: activeType, community: item.id })}
                scroll={false}
                className={activeCommunityId === item.id ? "community-link is-active" : "community-link"}
                aria-current={activeCommunityId === item.id ? "page" : undefined}
              >
                {item.name}
              </Link>
            ))}
            {communities.length === 0 ? <span className="side-empty">暂无社区</span> : null}
          </nav>
        </aside>

        <section className="home-main" aria-label="动态列表">
          <div className="page-bar">
            <div className="page-bar-left">
              <span className="page-label">{activeLabel}</span>
              {feed ? <span className="page-count">{feed.data.length} 条</span> : null}
            </div>
          </div>
          {feedResult.status === "rejected" ? <ErrorPanel error={feedResult.reason} /> : null}
          {feed && feed.data.length === 0 ? (
            <EmptyState>
              {activeCommunityId
                ? "这个社区还没有公开动态。"
                : feedPage > 1
                  ? "这一页没有内容。"
                  : "这个分类还没有公开动态。"}
            </EmptyState>
          ) : null}
          {feed ? (
            <div className="post-list" key={`${activeType ?? "all"}-${activeCommunityId ?? "all"}-${feedPage}`}>
              {feed.data.map((post, index) => (
                <PostRow
                  key={post.id}
                  post={post}
                  index={index}
                  communityName={post.community_id ? communityNames.get(post.community_id) : undefined}
                  from={feedPath}
                />
              ))}
            </div>
          ) : null}
        </section>

        <aside className="home-hot" aria-label="热度榜">
          <div className="side-head">
            <span className="side-title">热度</span>
            <span className="side-note">7 日</span>
          </div>
          {trending.length === 0 ? <p className="side-empty">暂无热度数据。</p> : null}
          <ol className="hot-list">
            {trending.map((post, index) => {
              const title = post.title?.trim() || excerpt(post.content, 40) || "图片帖子";
              const communityName = post.community_id ? communityNames.get(post.community_id) : undefined;
              return (
                <li className="hot-item" key={post.id}>
                  <span className="hot-rank">{String(index + 1).padStart(2, "0")}</span>
                  <div>
                    <Link href={withFrom(`/posts/${post.id}`, feedPath)} className="hot-title">{title}</Link>
                    <div className="hot-meta">
                      <span>{communityName ?? "公开动态"}</span>
                      <span>{((post.reaction_count ?? 0) * 2 + (post.comment_count ?? 0) * 3)} 热度</span>
                    </div>
                  </div>
                </li>
              );
            })}
          </ol>
        </aside>
      </div>

      {feed ? (
        <FeedPagination
          page={feedPage}
          hasMore={hasMore}
          type={activeType}
          communityId={activeCommunityId}
        />
      ) : null}
    </main>
  );
}
