"use client";

import Link from "next/link";

export default function ErrorPage({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return (
    <main className="simple-page">
      <div className="aside-label">503</div>
      <h1>暂时无法读取。</h1>
      <p>接口未返回数据，请稍后重试。</p>
      <div className="simple-actions">
        <button type="button" onClick={() => reset()}>
          重新读取
        </button>
        <Link href="/">返回动态</Link>
      </div>
    </main>
  );
}
