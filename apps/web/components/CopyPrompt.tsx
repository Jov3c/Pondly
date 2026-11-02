"use client";

import { useCallback, useRef, useState } from "react";

function legacyCopy(text: string) {
  const area = document.createElement("textarea");
  area.value = text;
  area.setAttribute("readonly", "");
  area.style.position = "fixed";
  area.style.opacity = "0";
  document.body.appendChild(area);
  area.select();
  const copied = document.execCommand("copy");
  area.remove();
  return copied;
}

export function CopyPrompt({ text }: { text: string }) {
  const [state, setState] = useState<"idle" | "copied" | "failed">("idle");
  const copyRef = useRef<() => void>(() => {});

  async function copy() {
    let copied = false;
    try {
      if (navigator.clipboard?.writeText) {
        copied = await Promise.race([
          navigator.clipboard.writeText(text).then(() => true).catch(() => false),
          new Promise<boolean>((resolve) => window.setTimeout(() => resolve(false), 700)),
        ]);
      }
    } catch {
      copied = false;
    }
    if (!copied) copied = legacyCopy(text);
    setState(copied ? "copied" : "failed");
    window.setTimeout(() => setState("idle"), copied ? 1800 : 3000);
  }
  copyRef.current = copy;

  const attach = useCallback((node: HTMLButtonElement | null) => {
    if (!node) return;
    node.onclick = () => copyRef.current();
  }, []);

  return (
    <button ref={attach} type="button" className="copy-prompt" aria-live="polite">
      {state === "copied" ? "已复制" : state === "failed" ? "复制失败" : "复制提示词"}
    </button>
  );
}
