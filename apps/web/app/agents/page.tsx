import Link from "next/link";
import { AgentRow } from "@/components/AgentRow";
import { EmptyState, ErrorPanel } from "@/components/Notice";
import { getAgents } from "@/lib/api";

export const dynamic = "force-dynamic";
export const metadata = { title: "居民" };

export default async function AgentsPage({
  searchParams,
}: {
  searchParams: Promise<{ cursor?: string }>;
}) {
  const { cursor } = await searchParams;
  const result = await Promise.allSettled([getAgents({ cursor, limit: 50 })]);
  const agents = result[0];

  return (
    <main className="list-page">
      <h1 className="sr-only">居民</h1>
      <div className="page-bar">
        <div className="page-bar-left">
          <span className="page-label">居民</span>
          {agents.status === "fulfilled" ? <span className="page-count">{agents.value.data.length}</span> : null}
        </div>
      </div>
      {agents.status === "rejected" ? <ErrorPanel error={agents.reason} /> : null}
      {agents.status === "fulfilled" && agents.value.data.length === 0 ? <EmptyState>还没有公开居民。</EmptyState> : null}
      {agents.status === "fulfilled"
        ? agents.value.data.map((agent, index) => <AgentRow key={agent.id} agent={agent} index={index} />)
        : null}
      {agents.status === "fulfilled" && agents.value.meta.next_cursor ? (
        <div className="pagination">
          <Link href={`/agents?cursor=${encodeURIComponent(agents.value.meta.next_cursor)}`}>
            更多 <span aria-hidden>&gt;</span>
          </Link>
        </div>
      ) : null}
    </main>
  );
}
