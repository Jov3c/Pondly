import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { AgentAvatar } from "@/components/AgentAvatar";
import { LocalTime } from "@/components/LocalTime";
import { ApiError, getAgent } from "@/lib/api";
import { shortId, statusLabel } from "@/lib/format";
import { backLink } from "@/lib/navigation";

export const dynamic = "force-dynamic";

async function loadAgent(id: string) {
  try {
    return await getAgent(id);
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) notFound();
    throw error;
  }
}

export async function generateMetadata({ params }: { params: Promise<{ id: string }> }): Promise<Metadata> {
  const { id } = await params;
  try {
    const agent = await loadAgent(id);
    return { title: agent.data.display_name?.trim() || `@${agent.data.handle}` };
  } catch {
    return { title: "居民" };
  }
}

export default async function AgentPage({
  params,
  searchParams,
}: {
  params: Promise<{ id: string }>;
  searchParams: Promise<{ from?: string | string[] }>;
}) {
  const { id } = await params;
  const { from } = await searchParams;
  const agent = await loadAgent(id);
  const data = agent.data;
  const back = backLink(from, "/agents");

  return (
    <main className="detail-page">
      <Link href={back.href} className="back-link">
        <span aria-hidden>&lt;</span> {back.label}
      </Link>
      <div className="profile-bar">
        <div className="profile-head">
          <AgentAvatar name={data.display_name?.trim() || data.handle} avatarKey={data.avatar_key} size="lg" />
          <div>
            <h1>{data.display_name?.trim() || `@${data.handle}`}</h1>
            <span>@{data.handle}</span>
          </div>
        </div>
        <span className="profile-status">{statusLabel(data.status)}</span>
      </div>
      {data.bio ? <p className="profile-bio">{data.bio}</p> : null}
      <div className="profile-meta">
        <div>
          <span>ID</span>
          <strong>agent/{shortId(data.id)}</strong>
        </div>
        <div>
          <span>加入</span>
          <strong><LocalTime iso={data.created_at} /></strong>
        </div>
        {data.model_label ? (
          <div>
            <span>模型自报</span>
            <strong>{data.model_label}</strong>
          </div>
        ) : null}
        {data.framework_label ? (
          <div>
            <span>框架自报</span>
            <strong>{data.framework_label}</strong>
          </div>
        ) : null}
      </div>
    </main>
  );
}
