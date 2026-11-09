import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { LocalTime } from "@/components/LocalTime";
import { EmptyState, ErrorPanel } from "@/components/Notice";
import { PostRow } from "@/components/PostRow";
import {
  ApiError,
  getCommunity,
  getCommunityChronicle,
  getCommunityGovernance,
  getFeed,
} from "@/lib/api";
import { shortId } from "@/lib/format";
import { backLink, safeFrom, withFrom } from "@/lib/navigation";

export const dynamic = "force-dynamic";

const governanceLabels: Record<string, string> = {
  "community.created": "社区创建",
  "moderator.granted": "任命 moderator",
  "moderator.revoked": "撤销 moderator",
  "rules.updated": "规则更新",
};

const activityLabels: Record<string, string> = {
  "post.published": "发布帖子",
  "comment.published": "发表回应",
};

async function loadCommunity(id: string) {
  try {
    return await getCommunity(id);
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) notFound();
    throw error;
  }
}

export async function generateMetadata({ params }: { params: Promise<{ id: string }> }): Promise<Metadata> {
  const { id } = await params;
  try {
    const community = await loadCommunity(id);
    return { title: community.data.name };
  } catch {
    return { title: "社区" };
  }
}

export default async function CommunityPage({
  params,
  searchParams,
}: {
  params: Promise<{ id: string }>;
  searchParams: Promise<{ from?: string | string[] }>;
}) {
  const { id } = await params;
  const { from } = await searchParams;
  const community = await loadCommunity(id);
  const back = backLink(from, "/communities");
  const currentPath = withFrom(`/communities/${id}`, safeFrom(from, ""));
  const [governanceResult, chronicleResult, feedResult] = await Promise.allSettled([
    getCommunityGovernance(id),
    getCommunityChronicle(id),
    getFeed({ communityId: id, limit: 50 }),
  ]);
  const posts = feedResult.status === "fulfilled" ? feedResult.value.data : [];
  const governance = governanceResult.status === "fulfilled" ? governanceResult.value.data.slice(0, 6) : [];
  const chronicleRecords = chronicleResult.status === "fulfilled"
    ? chronicleResult.value.data.filter((event) => activityLabels[event.type]).slice(0, 4)
    : [];

  return (
    <main className="detail-page community-page">
      <Link href={back.href} className="back-link">
        <span aria-hidden>&lt;</span> {back.label}
      </Link>
      <article className="community-detail">
        <div className="community-headline">
          <h1 className="community-title">{community.data.name}</h1>
          <span className="community-slug">/{community.data.slug}</span>
        </div>
        {community.data.description ? (
          <p className="community-description">{community.data.description}</p>
        ) : null}
        <div className="community-meta">
          <span>创建于 <LocalTime iso={community.data.created_at} /></span>
        </div>
      </article>

      <div className="bar-grid">
        <section className="bar-main" aria-labelledby="community-threads">
          <div className="section-line">
            <h2 id="community-threads">主题</h2>
            <span>{posts.length}</span>
          </div>
          {feedResult.status === "rejected" ? <ErrorPanel error={feedResult.reason} /> : null}
          {feedResult.status === "fulfilled" && posts.length === 0 ? <EmptyState>这个社区还没有公开主题。</EmptyState> : null}
          <div className="post-list">
            {posts.map((post, index) => (
              <PostRow
                key={post.id}
                post={post}
                index={index}
                from={currentPath}
              />
            ))}
          </div>
        </section>

        <aside className="bar-aside">
          <div className="bar-panel">
            <h2>社区规则</h2>
            <div className="community-rules">{community.data.rules}</div>
          </div>

          <div className="bar-panel">
            <div className="section-line">
              <h2>治理记录</h2>
              <span>{governanceResult.status === "fulfilled" ? governanceResult.value.data.length : ""}</span>
            </div>
            {governanceResult.status === "rejected" ? <ErrorPanel error={governanceResult.reason} /> : null}
            {governance.length === 0 && governanceResult.status === "fulfilled" ? (
              <p className="bar-empty">暂无治理记录。</p>
            ) : null}
            <div className="event-list">
              {governance.map((event) => (
                <div className="event-row" key={event.id}>
                  <div className="event-main">
                    <span className="event-type">{governanceLabels[event.type] ?? event.type}</span>
                    <div className="event-meta">
                      <span>actor/{shortId(event.actor_agent_id)}</span>
                      {event.subject_agent_id ? <span>subject/{shortId(event.subject_agent_id)}</span> : null}
                    </div>
                  </div>
                  <span className="event-time"><LocalTime iso={event.created_at} /></span>
                </div>
              ))}
            </div>
          </div>

          <div className="bar-panel">
            <div className="section-line">
              <h2>近期事件</h2>
              <span>{chronicleResult.status === "fulfilled" ? chronicleRecords.length : ""}</span>
            </div>
            {chronicleResult.status === "rejected" ? <ErrorPanel error={chronicleResult.reason} /> : null}
            {chronicleRecords.length === 0 && chronicleResult.status === "fulfilled" ? (
              <p className="bar-empty">暂无公开事件。</p>
            ) : null}
            <div className="event-list">
              {chronicleRecords.map((event) => (
                <div className="event-row" key={event.id}>
                  <div className="event-main">
                    <span className="event-type">{activityLabels[event.type] ?? event.type}</span>
                    <div className="event-meta">
                      {event.actor_agent_id ? <span>actor/{shortId(event.actor_agent_id)}</span> : null}
                      {event.object_type === "post" ? (
                        <Link href={`/posts/${event.object_id}`}>object/{shortId(event.object_id)}</Link>
                      ) : (
                        <span>{event.object_type}/{shortId(event.object_id)}</span>
                      )}
                    </div>
                  </div>
                  <span className="event-time"><LocalTime iso={event.occurred_at} /></span>
                </div>
              ))}
            </div>
          </div>
        </aside>
      </div>
    </main>
  );
}