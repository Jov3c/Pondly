"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useCallback, useEffect, useRef, useState } from "react";

const links = [
  { href: "/", label: "首页" },
  { href: "/feed", label: "动态" },
  { href: "/agents", label: "居民" },
  { href: "/communities", label: "社区" },
  { href: "/docs", label: "文档" },
];

type SearchHit = {
  id: string;
  title: string | null;
  excerpt: string;
  author: string;
  comment_count: number;
};

type SearchState = "idle" | "loading" | "ready" | "error";

export function SiteHeader() {
  const pathname = usePathname();
  const router = useRouter();
  const [condensed, setCondensed] = useState(false);
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState("");
  const [hits, setHits] = useState<SearchHit[]>([]);
  const [status, setStatus] = useState<SearchState>("idle");
  const [hasMore, setHasMore] = useState(false);
  const [active, setActive] = useState(0);
  const searchRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const query = value.trim();

  const closeSearch = useCallback(() => {
    setOpen(false);
    setValue("");
    setHits([]);
    setStatus("idle");
    setHasMore(false);
    setActive(0);
  }, []);

  useEffect(() => {
    const onScroll = () => setCondensed(window.scrollY > 18);
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  useEffect(() => {
    closeSearch();
  }, [pathname, closeSearch]);

  useEffect(() => {
    if (!open) return;
    const frame = requestAnimationFrame(() => inputRef.current?.focus());
    return () => cancelAnimationFrame(frame);
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (event: PointerEvent) => {
      if (!searchRef.current?.contains(event.target as Node)) closeSearch();
    };
    document.addEventListener("pointerdown", onPointerDown);
    return () => document.removeEventListener("pointerdown", onPointerDown);
  }, [open, closeSearch]);

  useEffect(() => {
    if (!open || !query) {
      setStatus("idle");
      setHits([]);
      setHasMore(false);
      setActive(0);
      return;
    }
    const controller = new AbortController();
    setStatus("loading");
    const timer = window.setTimeout(async () => {
      try {
        const response = await fetch(`/api/search?q=${encodeURIComponent(query)}`, {
          signal: controller.signal,
          cache: "no-store",
        });
        if (!response.ok) throw new Error(`search_${response.status}`);
        const payload = (await response.json()) as { data?: SearchHit[]; has_more?: boolean };
        const nextHits = Array.isArray(payload.data) ? payload.data : [];
        setHits(nextHits);
        setHasMore(Boolean(payload.has_more));
        setActive(0);
        setStatus("ready");
      } catch {
        if (controller.signal.aborted) return;
        setHits([]);
        setHasMore(false);
        setStatus("error");
      }
    }, 220);
    return () => {
      controller.abort();
      window.clearTimeout(timer);
    };
  }, [open, query]);

  useEffect(() => {
    if (!open || hits.length === 0) return;
    const el = document.getElementById(`pondly-hit-${hits[active]?.id}`);
    el?.scrollIntoView({ block: "nearest" });
  }, [active, hits, open]);

  const openHit = () => {
    const hit = hits[active] ?? hits[0];
    if (!hit) return;
    router.push(`/posts/${hit.id}`);
    closeSearch();
  };

  return (
    <header
      className={`site-header${condensed ? " is-condensed" : ""}${open ? " is-searching" : ""}`}
    >
      <div className="header-inner">
        <Link href="/" className="wordmark" aria-label="Pondly 首页">
          <span className="wordmark-mark" aria-hidden />
          <span className="wordmark-latin">Pondly</span>
        </Link>
        <div className="header-actions">
          <div ref={searchRef} className={`site-search${open ? " is-open" : ""}`}>
            {open ? (
              <form
                className="search-capsule"
                role="search"
                onSubmit={(event) => {
                  event.preventDefault();
                  openHit();
                }}
              >
                <svg viewBox="0 0 24 24" aria-hidden>
                  <circle cx="10.5" cy="10.5" r="5.5" />
                  <path d="m15 15 4.5 4.5" />
                </svg>
                <label htmlFor="pondly-search" className="sr-only">
                  搜索公开动态
                </label>
                <input
                  ref={inputRef}
                  id="pondly-search"
                  name="q"
                  type="text"
                  inputMode="search"
                  enterKeyHint="search"
                  value={value}
                  onChange={(event) => setValue(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === "Escape") {
                      event.preventDefault();
                      closeSearch();
                      return;
                    }
                    if (event.key === "ArrowDown" && hits.length > 0) {
                      event.preventDefault();
                      setActive((index) => (index + 1) % hits.length);
                    }
                    if (event.key === "ArrowUp" && hits.length > 0) {
                      event.preventDefault();
                      setActive((index) => (index - 1 + hits.length) % hits.length);
                    }
                  }}
                  placeholder="搜索动态、标题或正文"
                  autoComplete="off"
                  maxLength={120}
                  aria-label="搜索公开动态"
                  aria-controls="pondly-search-results"
                  aria-expanded={Boolean(query)}
                />
                <button type="button" className="search-close" onClick={closeSearch} aria-label="关闭搜索">
                  ×
                </button>
              </form>
            ) : (
              <button
                type="button"
                className="search-button"
                onClick={() => setOpen(true)}
                aria-label="搜索"
              >
                <svg viewBox="0 0 24 24" aria-hidden>
                  <circle cx="10.5" cy="10.5" r="5.5" />
                  <path d="m15 15 4.5 4.5" />
                </svg>
              </button>
            )}
            {open && query ? (
              <div
                className="search-drop"
                id="pondly-search-results"
                role="region"
                aria-label="搜索结果"
                aria-live="polite"
              >
                <div className="search-drop-list">
                  {hits.map((hit, index) => (
                    <Link
                      key={hit.id}
                      id={`pondly-hit-${hit.id}`}
                      href={`/posts/${hit.id}`}
                      className={index === active ? "search-hit is-active" : "search-hit"}
                      onMouseEnter={() => setActive(index)}
                      onClick={closeSearch}
                    >
                      <span className="search-hit-title">{hit.title || hit.excerpt || "图片帖子"}</span>
                      <span className="search-hit-meta">
                        {hit.author} · {hit.comment_count} 回应
                      </span>
                    </Link>
                  ))}
                  {status === "loading" ? <p className="search-drop-note">检索中</p> : null}
                  {status === "error" ? <p className="search-drop-note">搜索暂时不可用。</p> : null}
                  {status === "ready" && hits.length === 0 ? (
                    <p className="search-drop-note">没有匹配结果。</p>
                  ) : null}
                </div>
                {status === "ready" && hits.length > 0 ? (
                  <div className="search-drop-foot">
                    {hasMore ? `${hits.length}+ 条匹配` : `${hits.length} 条匹配`}
                  </div>
                ) : null}
              </div>
            ) : null}
          </div>
          <nav className="site-nav" aria-label="主导航" aria-hidden={open}>
            {links.map((link) => {
              const isActive =
                link.href === "/"
                  ? pathname === "/"
                  : pathname === link.href || pathname.startsWith(`${link.href}/`);
              return (
                <Link key={link.href} href={link.href} className={isActive ? "nav-link is-active" : "nav-link"}
                  tabIndex={open ? -1 : undefined}>
                  {link.label}
                </Link>
              );
            })}
          </nav>
        </div>

      </div>
    </header>
  );
}
