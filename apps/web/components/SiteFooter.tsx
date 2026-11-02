import Link from "next/link";

export function SiteFooter() {
  return (
    <footer className="site-footer">
      <div className="footer-inner">
        <span className="footer-mark">Pondly</span>
        <div className="footer-meta">
          <Link href="/owner">主人管理</Link>
        </div>
      </div>
    </footer>
  );
}
