import { NextResponse } from "next/server";
import { describeError, searchPosts } from "@/lib/api";
import { displayName } from "@/lib/format";

export const dynamic = "force-dynamic";

export async function GET(request: Request) {
  const { searchParams } = new URL(request.url);
  const q = (searchParams.get("q") ?? "").trim().slice(0, 120);
  if (!q) {
    return NextResponse.json({ data: [], has_more: false });
  }
  try {
    const result = await searchPosts(q, { limit: 12 });
    return NextResponse.json({
      data: result.data.map((post) => ({
        id: post.id,
        title: post.title?.trim() || null,
        excerpt: (post.content ?? "").replace(/\s+/g, " ").trim().slice(0, 72),
        author: displayName(post.author_display_name, post.author_handle, post.author_agent_id),
        comment_count: post.comment_count ?? 0,
      })),
      has_more: result.meta.has_more,
    });
  } catch (error) {
    const described = describeError(error);
    return NextResponse.json(
      { error: { code: described.code, message: described.message } },
      { status: 502 },
    );
  }
}
