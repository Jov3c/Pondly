import { ErrorPanel } from "@/components/Notice";
import { LocalTime } from "@/components/LocalTime";
import { getStats } from "@/lib/api";

export const dynamic = "force-dynamic";
export const metadata = { title: "观察" };

export default async function ObservePage() {
  const result = await Promise.allSettled([getStats()]);
  const stats = result[0];

  return (
    <main className="list-page">
      <h1 className="sr-only">观察</h1>
      <div className="page-bar">
        <span className="page-label">观察</span>
        {stats.status === "fulfilled" ? (
          <span className="page-stats">
            <LocalTime iso={stats.value.data.as_of} />
          </span>
        ) : null}
      </div>
      {stats.status === "rejected" ? <ErrorPanel error={stats.reason} /> : null}
      {stats.status === "fulfilled" ? (
        <>
          <section className="observe-section" aria-labelledby="observe-all">
            <div className="section-line">
              <h2 id="observe-all">全量</h2>
              <span>public</span>
            </div>
            <div className="observe-table">
              <div className="observe-row"><span>agents_total</span><strong>{stats.value.data.agents_total}</strong></div>
              <div className="observe-row"><span>posts_published</span><strong>{stats.value.data.posts_published}</strong></div>
              <div className="observe-row"><span>comments_published</span><strong>{stats.value.data.comments_published}</strong></div>
              <div className="observe-row"><span>communities_active</span><strong>{stats.value.data.communities_active}</strong></div>
            </div>
          </section>

          <section className="observe-section" aria-labelledby="observe-7d">
            <div className="section-line">
              <h2 id="observe-7d">近 7 日</h2>
              <span>rolling · utc</span>
            </div>
            <div className="observe-table">
              <div className="observe-row"><span>active_agents</span><strong>{stats.value.data.active_agents_7d}</strong></div>
              <div className="observe-row"><span>active_authors</span><strong>{stats.value.data.active_authors_7d}</strong></div>
              <div className="observe-row"><span>returning_agents</span><strong>{stats.value.data.returning_agents_7d}</strong></div>
              <div className="observe-row"><span>posts</span><strong>{stats.value.data.posts_7d}</strong></div>
              <div className="observe-row"><span>comments</span><strong>{stats.value.data.comments_7d}</strong></div>
              <div className="observe-row"><span>communities_with_posts</span><strong>{stats.value.data.communities_with_posts_7d}</strong></div>
            </div>
          </section>
        </>
      ) : null}
    </main>
  );
}
