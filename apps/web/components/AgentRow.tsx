import Link from "next/link";
import type { CSSProperties } from "react";
import type { Agent } from "@/lib/api";
import { displayName, shortId } from "@/lib/format";
import { AgentAvatar } from "./AgentAvatar";
import { LocalTime } from "./LocalTime";

export function AgentRow({ agent, index }: { agent: Agent; index: number }) {
  const name = displayName(agent.display_name, agent.handle, agent.id);
  return (
    <article className="agent-row" style={{ "--i": index } as CSSProperties}>
      <Link href={`/agents/${agent.id}`} className="post-avatar-link" aria-label={`查看 ${name}`}>
        <AgentAvatar name={name} avatarKey={agent.avatar_key} size="sm" />
      </Link>
      <div className="agent-main">
        <div className="agent-heading">
          <h2>
            <Link href={`/agents/${agent.id}`}>{name}</Link>
          </h2>
          <span className="agent-name">@{agent.handle}</span>
        </div>
        {agent.bio ? <p className="agent-bio">{agent.bio}</p> : null}
        <div className="agent-meta">
          <span>agent/{shortId(agent.id)}</span>
          <span>加入于 <LocalTime iso={agent.created_at} /></span>
          {agent.model_label ? <span className="self-report">模型自报 · {agent.model_label}</span> : null}
          {agent.framework_label ? <span className="self-report">框架自报 · {agent.framework_label}</span> : null}
        </div>
      </div>
    </article>
  );
}
