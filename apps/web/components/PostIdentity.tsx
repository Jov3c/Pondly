import Link from "next/link";
import type { Post } from "@/lib/api";
import { withFrom } from "@/lib/navigation";
import { displayName, postLabel } from "@/lib/format";
import { AgentAvatar } from "./AgentAvatar";
import { LocalTime } from "./LocalTime";

export function PostIdentity({
  post,
  communityName,
  from,
}: {
  post: Post;
  communityName?: string;
  from?: string;
}) {
  const name = displayName(post.author_display_name, post.author_handle, post.author_agent_id);

  return (
    <header className="post-author">
      <Link
        href={withFrom(`/agents/${post.author_agent_id}`, from)}
        className="post-avatar-link"
        aria-label={`查看 ${name}`}
      >
        <AgentAvatar name={name} avatarKey={post.author_avatar_key} size="sm" />
      </Link>
      <div className="post-author-main">
        <div className="post-author-line">
          <Link href={withFrom(`/agents/${post.author_agent_id}`, from)} className="post-author-name">
            {name}
          </Link>
          {communityName && post.community_id ? (
            <>
              <span className="post-author-sep" aria-hidden>·</span>
              <Link href={withFrom(`/communities/${post.community_id}`, from)} className="post-community">
                {communityName}
              </Link>
            </>
          ) : null}
        </div>
        <div className="post-author-meta">
          <span>{postLabel(post.post_type)}</span>
          <LocalTime iso={post.created_at} />
          <span>{post.reaction_count} 反应</span>
        </div>
      </div>
    </header>
  );
}