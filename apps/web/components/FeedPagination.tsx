"use client";

import { useRouter } from "next/navigation";
import { useEffect, useMemo, useRef, useState } from "react";

function Chevron({ direction }: { direction: "left" | "right" }) {
  return (
    <svg viewBox="0 0 24 24" aria-hidden>
      <path d={direction === "left" ? "m14 6-6 6 6 6" : "m10 6 6 6-6 6"} />
    </svg>
  );
}

function hrefFor(page: number, type?: "moment" | "discussion") {
  const params = new URLSearchParams();
  if (type) params.set("type", type);
  params.set("page", String(page));
  return `/?${params.toString()}`;
}

export function FeedPagination({
  page,
  hasMore,
  type,
}: {
  page: number;
  hasMore: boolean;
  type?: "moment" | "discussion";
}) {
  const router = useRouter();
  const slotRef = useRef<HTMLDivElement>(null);
  const pagesRef = useRef<HTMLDivElement>(null);
  const timerRef = useRef<number | null>(null);
  const [docked, setDocked] = useState(true);
  const [displayPage, setDisplayPage] = useState(page);
  const [navigating, setNavigating] = useState(false);
  const [indicator, setIndicator] = useState({ left: 0, width: 30 });

  const pages = useMemo(() => {
    const start = Math.max(1, page - 1);
    const end = hasMore ? page + 1 : page;
    return Array.from({ length: end - start + 1 }, (_, index) => start + index);
  }, [page, hasMore]);

  useEffect(() => {
    setDisplayPage(page);
    setNavigating(false);
  }, [page]);

  useEffect(() => {
    const slot = slotRef.current;
    if (!slot || typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver(
      ([entry]) => setDocked(entry.isIntersecting),
      { rootMargin: "0px 0px -10% 0px", threshold: 0.2 },
    );
    observer.observe(slot);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    const container = pagesRef.current;
    const target = container?.querySelector<HTMLElement>(`[data-page="${displayPage}"]`);
    if (!target) return;
    setIndicator({ left: target.offsetLeft, width: target.offsetWidth });
  }, [displayPage, pages]);

  useEffect(() => () => {
    if (timerRef.current) window.clearTimeout(timerRef.current);
  }, []);

  if (page === 1 && !hasMore) return null;

  function navigate(target: number) {
    if (navigating || target < 1 || target === page) return;
    setDisplayPage(target);
    setNavigating(true);
    timerRef.current = window.setTimeout(() => {
      router.push(hrefFor(target, type));
    }, 200);
  }

  return (
    <div ref={slotRef} className="pagination-slot">
      <nav
        className={docked ? "pagination-capsule is-docked" : "pagination-capsule is-floating"}
        aria-label="翻页"
        aria-busy={navigating}
      >
        <button
          type="button"
          className="pagination-arrow"
          onClick={() => navigate(page - 1)}
          disabled={page <= 1 || navigating}
          aria-label="上一页"
        >
          <Chevron direction="left" />
        </button>
        <div ref={pagesRef} className="pagination-pages">
          <span
            className="pagination-indicator"
            aria-hidden
            style={{ width: indicator.width, transform: `translateX(${indicator.left}px)` }}
          />
          {pages.map((number) => (
            <button
              key={number}
              type="button"
              data-page={number}
              className={number === displayPage ? "pagination-page is-current" : "pagination-page"}
              onClick={() => navigate(number)}
              disabled={number === displayPage || navigating}
              aria-current={number === displayPage ? "page" : undefined}
            >
              {number}
            </button>
          ))}
        </div>
        <button
          type="button"
          className="pagination-arrow"
          onClick={() => navigate(page + 1)}
          disabled={!hasMore || navigating}
          aria-label="下一页"
        >
          <Chevron direction="right" />
        </button>
      </nav>
    </div>
  );
}
