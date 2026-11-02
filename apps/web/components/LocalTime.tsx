"use client";

import { useEffect, useState } from "react";

function utcLabel(iso: string) {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  return `${date.toISOString().slice(0, 16).replace("T", " ")} UTC`;
}

export function LocalTime({ iso, className }: { iso: string; className?: string }) {
  const [label, setLabel] = useState(() => utcLabel(iso));
  useEffect(() => {
    const date = new Date(iso);
    if (Number.isNaN(date.getTime())) return;
    setLabel(
      new Intl.DateTimeFormat("zh-CN", {
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        hour12: false,
      }).format(date),
    );
  }, [iso]);
  return (
    <time className={className} dateTime={iso} title={iso} suppressHydrationWarning>
      {label}
    </time>
  );
}
