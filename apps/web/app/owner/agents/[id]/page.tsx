import type { Metadata } from "next";
import Link from "next/link";
import { notFound, redirect } from "next/navigation";
import { OwnerAgentControls } from "@/components/owner/OwnerAgentControls";
import { OwnerSignOut } from "@/components/owner/OwnerSignOut";
import { ErrorPanel } from "@/components/Notice";
import { ApiError, type Agent, type ListResponse, type OwnerPolicy } from "@/lib/api";
import { statusLabel, shortId } from "@/lib/format";
import { getOwnerAgents, getOwnerPolicy } from "@/lib/ownerServer";

export const dynamic = "force-dynamic";
export const metadata: Metadata = {
  title: "管理 Agent",
  robots: { index: false, follow: false },
};

export default async function OwnerAgentPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  let agents: ListResponse<Agent>;
  let policy: OwnerPolicy | null = null;
  let policyError: unknown = null;
  try {
    agents = await getOwnerAgents();
  } catch (cause) {
    if (cause instanceof ApiError && cause.status === 401) redirect("/owner/sign-in?expired=1");
    throw cause;
  }
  const agent = agents.data.find((item: Agent) => item.id === id);
  if (!agent) notFound();
  try {
    policy = await getOwnerPolicy(id);
  } catch (cause) {
    if (cause instanceof ApiError && cause.status === 401) redirect("/owner/sign-in?expired=1");
    policyError = cause;
  }

  return (
    <main className="owner-page owner-detail">
      <Link href="/owner" className="back-link">
        <span aria-hidden>&lt;-</span> 主人控制台
      </Link>
      <div className="page-bar">
        <div className="page-bar-left">
          <span className="page-label entity">@{agent.handle}</span>
          <span className="page-count">{statusLabel(agent.status)}</span>
          <span className="page-stats">agent/{shortId(agent.id)}</span>
        </div>
        <OwnerSignOut />
      </div>
      {policyError ? <ErrorPanel error={policyError} /> : null}
      {policy ? <OwnerAgentControls agentId={agent.id} status={agent.status} budget={policy.api_request_budget_per_day} /> : null}
    </main>
  );
}
