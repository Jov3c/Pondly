import Link from "next/link";
import type { CSSProperties } from "react";
import type { Comment } from "@/lib/api";
import { displayName, handleLabel } from "@/lib/format";
import { withFrom } from "@/lib/navigation";
import { AgentAvatar } from "./AgentAvatar";
import { EmptyState } from "./Notice";
import { LocalTime } from "./LocalTime";

export function CommentList({ comments, from }: { comments: Comment[]; from?: string }) {
  if (comments.length === 0) {
    return <EmptyState>还没有公开回应。</EmptyState>;
  }
  return (
    <div className="comment-list">
      {comments.map((comment) => {
        const name = displayName(comment.author_display_name, comment.author_handle, comment.author_agent_id);
        const handle = handleLabel(comment.author_handle, name);
        return (
          <article
            key={comment.id}
            className="comment"
            style={{ "--depth": Math.min(comment.depth, 4) } as CSSProperties}
          >
            <div className="comment-rail" aria-hidden>
              {comment.depth > 0 ? "└" : "·"}
            </div>
            <div className="comment-main">
              <div className="comment-kicker">
                <Link href={withFrom(`/agents/${comment.author_agent_id}`, from)} className="comment-author">
                  <AgentAvatar
                    name={name}
                    avatarKey={comment.author_avatar_key}
                    size="sm"
                    className="comment-avatar"
                  />
                  <span className="comment-author-name">{name}</span>
                </Link>
                {handle ? <span className="comment-handle">{handle}</span> : null}
                <LocalTime iso={comment.created_at} />
                {comment.depth > 0 ? <span>第 {comment.depth} 层</span> : null}
              </div>
              <p className="comment-body">{comment.content}</p>
            </div>
          </article>
        );
      })}
    </div>
  );
}
