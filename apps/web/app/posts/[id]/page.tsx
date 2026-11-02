import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { CommentList } from "@/components/CommentList";
import { ErrorPanel } from "@/components/Notice";
import { PostIdentity } from "@/components/PostIdentity";
import { PostImages } from "@/components/PostImages";
import { ApiError, getComments, getCommunity, getPost } from "@/lib/api";
import { statusLabel, postLabel } from "@/lib/format";

export const dynamic = "force-dynamic";

async function loadPost(id: string) {
  try {
    return await getPost(id);
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) notFound();
    throw error;
  }
}

export async function generateMetadata({ params }: { params: Promise<{ id: string }> }): Promise<Metadata> {
  const { id } = await params;
  try {
    const post = await loadPost(id);
    return { title: post.data.title || postLabel(post.data.post_type) };
  } catch {
    return { title: "帖子" };
  }
}

export default async function PostPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const post = await loadPost(id);
  const [commentsResult, communityResult] = await Promise.allSettled([
    getComments(id, { limit: 50 }),
    post.data.community_id ? getCommunity(post.data.community_id) : Promise.resolve(null),
  ]);
  const comments = commentsResult;
  const communityName =
    communityResult.status === "fulfilled" && communityResult.value
      ? communityResult.value.data.name
      : undefined;
  const images = post.data.image_urls ?? [];

  return (
    <main className="detail-page">
      <Link href="/" className="back-link">
        <span aria-hidden>&lt;-</span> 动态
      </Link>
      <article className="post-detail">
        <PostIdentity post={post.data} communityName={communityName} />
        {post.data.title ? (
          <h1 className="detail-title">{post.data.title}</h1>
        ) : (
          <h1 className="sr-only">帖子</h1>
        )}
        {post.data.content ? <div className="post-body">{post.data.content}</div> : null}
        <PostImages urls={images} />
        <div className="post-footer">
          <span>{post.data.comment_count} 回应</span>
          <span>{post.data.reaction_count} 反应</span>
          <span>{statusLabel(post.data.status)}</span>
        </div>
      </article>

      <section className="comments-section" aria-labelledby="comments-heading">
        <div className="section-line">
          <h2 id="comments-heading">回应</h2>
          <span>{comments.status === "fulfilled" ? comments.value.data.length : ""}</span>
        </div>
        {comments.status === "rejected" ? <ErrorPanel error={comments.reason} /> : null}
        {comments.status === "fulfilled" ? <CommentList comments={comments.value.data} /> : null}
      </section>
    </main>
  );
}
