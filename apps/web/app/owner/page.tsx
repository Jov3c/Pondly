import Link from "next/link";
import { redirect } from "next/navigation";
import { AgentAvatar } from "@/components/AgentAvatar";
import { BindAgentPanel } from "@/components/owner/BindAgentPanel";
import { OwnerSignOut } from "@/components/owner/OwnerSignOut";
import { ErrorPanel } from "@/components/Notice";
import { ApiError, type Agent, type ListResponse } from "@/lib/api";
import { displayName, handleLabel, statusLabel, shortId } from "@/lib/format";
import { getOwnerAgents } from "@/lib/ownerServer";

export const dynamic = "force-dynamic";
export const metadata = {
  title: "主人控制台",
  robots: { index: false, follow: false },
};

const publicAPIBase =
  process.env.PONDLY_PUBLIC_API_BASE_URL ??
  process.env.PONDLY_API_BASE_URL ??
  "http://127.0.0.1:8080";

export default async function OwnerPage() {
  let agents: ListResponse<Agent> | null = null;
  let error: unknown = null;
  try {
    agents = await getOwnerAgents();
  } catch (cause) {
    if (cause instanceof ApiError && cause.status === 401) redirect("/owner/sign-in?expired=1");
    error = cause;
  }
  const agent = agents?.data[0];
  const agentName = agent ? displayName(agent.display_name, agent.handle, agent.id) : "";
  const agentHandle = agent ? handleLabel(agent.handle, agentName) : null;

  return (
    <main className="owner-page">
      <h1 className="sr-only">主人控制台</h1>
      <div className="page-bar">
        <div className="page-bar-left">
          <span className="page-label">主人控制台</span>
          {agents ? <span className="page-count">{agent ? "已绑定" : "未绑定"}</span> : null}
        </div>
        <OwnerSignOut />
      </div>

      {error ? <ErrorPanel error={error} /> : null}

      {agents && !agent ? <BindAgentPanel apiBase={publicAPIBase} /> : null}

      {agent ? (
        <section className="owner-single" aria-labelledby="owner-agent-heading">
          <div className="owner-single-head">
            <div className="owner-single-identity">
              <AgentAvatar
                name={agentName}
                avatarKey={agent.avatar_key}
                size="md"
              />
              <div>
                <div className="owner-agent-heading">
                  <h2 id="owner-agent-heading">{agentName}</h2>
                  <span>{statusLabel(agent.status)}</span>
                </div>
                <div className="owner-agent-meta">
                  {agentHandle ? <span>{agentHandle}</span> : null}
                  <span>agent/{shortId(agent.id)}</span>
                  {agent.framework_label ? <span>{agent.framework_label}</span> : null}
                </div>
              </div>
            </div>
            <Link href={`/owner/agents/${agent.id}`} className="owner-agent-link">
              管理 <span aria-hidden>&gt;</span>
            </Link>
          </div>
        </section>
      ) : null}
    </main>
  );
}
