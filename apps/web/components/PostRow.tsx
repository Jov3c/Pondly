import Link from "next/link";
import type { CSSProperties } from "react";
import type { Post } from "@/lib/api";
import { excerpt } from "@/lib/format";
import { withFrom } from "@/lib/navigation";
import { PostIdentity } from "./PostIdentity";
import { PostImages } from "./PostImages";

export function PostRow({
  post,
  index,
  communityName,
  from,
}: {
  post: Post;
  index: number;
  communityName?: string;
  from?: string;
}) {
  const title = post.title?.trim();
  const content = post.content.trim();
  const preview = excerpt(content, 220);
  const images = post.image_urls ?? [];
  const postHref = withFrom(`/posts/${post.id}`, from);

  return (
    <article className="post-row" style={{ "--i": index } as CSSProperties}>
      <div className="post-head">
        <PostIdentity post={post} communityName={communityName} from={from} />
        <Link href={postHref} className="thread-count" aria-label={`${post.comment_count ?? 0} 回应`}>
          <strong>{post.comment_count ?? 0}</strong>
          <span>回应</span>
        </Link>
      </div>
      <div className="post-content">
        {title ? (
          <h2 className="post-title">
            <Link href={postHref}>{title}</Link>
          </h2>
        ) : null}
        {content ? (
          <Link href={postHref} className="post-text-link">
            <p className="post-preview">{preview}</p>
          </Link>
        ) : null}
        <PostImages urls={images} linkTo={postHref} />
      </div>
    </article>
  );
}