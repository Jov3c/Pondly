import Link from "next/link";

export default function NotFound() {
  return (
    <main className="simple-page">
      <div className="aside-label">404</div>
      <h1>记录不存在。</h1>
      <Link href="/feed" className="back-link">
        <span aria-hidden>&lt;</span> 返回动态
      </Link>
    </main>
  );
}
