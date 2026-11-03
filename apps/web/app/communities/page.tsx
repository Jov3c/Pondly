import type { Metadata } from "next";
import Link from "next/link";
import { LocalTime } from "@/components/LocalTime";
import { EmptyState, ErrorPanel } from "@/components/Notice";
import { getCommunities } from "@/lib/api";

export const dynamic = "force-dynamic";
export const metadata: Metadata = { title: "社区" };

export default async function CommunitiesPage({
  searchParams,
}: {
  searchParams: Promise<{ cursor?: string }>;
}) {
  const { cursor } = await searchParams;
  const result = await Promise.allSettled([getCommunities({ cursor, limit: 50 })]);
  const communities = result[0];

  return (
    <main className="list-page">
      <h1 className="sr-only">社区</h1>
      <div className="page-bar">
        <div className="page-bar-left">
          <span className="page-label">社区</span>
          {communities.status === "fulfilled" ? (
            <span className="page-count">{communities.value.data.length}</span>
          ) : null}
        </div>
      </div>
      {communities.status === "rejected" ? <ErrorPanel error={communities.reason} /> : null}
      {communities.status === "fulfilled" && communities.value.data.length === 0 ? (
        <EmptyState>还没有公开社区。</EmptyState>
      ) : null}
      {communities.status === "fulfilled" ? (
        <div className="community-list">
          {communities.value.data.map((community, index) => (
            <article className="community-row" key={community.id}>
              <div className="community-index" aria-hidden>
                {String(index + 1).padStart(2, "0")}
              </div>
              <div>
                <div className="community-heading">
                  <h2>
                    <Link href={`/communities/${community.id}`}>{community.name}</Link>
                  </h2>
                  <Link href={`/communities/${community.id}`} className="community-arrow" aria-label="打开社区">
                    <span aria-hidden>&gt;</span>
                  </Link>
                </div>
                {community.description ? (
                  <p className="community-description">{community.description}</p>
                ) : null}
                <div className="community-meta">
                  <span>/{community.slug}</span>
                  <span>
                    创建于 <LocalTime iso={community.created_at} />
                  </span>
                </div>
              </div>
            </article>
          ))}
        </div>
      ) : null}
      {communities.status === "fulfilled" && communities.value.meta.next_cursor ? (
        <div className="pagination">
          <Link href={`/communities?cursor=${encodeURIComponent(communities.value.meta.next_cursor)}`}>
            更多 <span aria-hidden>&gt;</span>
          </Link>
        </div>
      ) : null}
    </main>
  );
}