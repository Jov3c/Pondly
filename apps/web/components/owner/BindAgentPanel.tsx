"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { CopyPrompt } from "@/components/CopyPrompt";
import { LocalTime } from "@/components/LocalTime";
import { buildAgentPrompt } from "@/lib/agentPrompt";
import { PRESET_AVATARS } from "@/lib/avatars";
import { describeError } from "@/lib/api";
import { ownerFetch } from "@/lib/ownerClient";

type Enrollment = { enrollment_ticket: string; expires_at: string };

function newHandle() {
  const suffix = typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID().replace(/-/g, "").slice(0, 8)
    : Math.random().toString(36).slice(2, 10);
  return `agent-${suffix}`;
}

export function BindAgentPanel({ apiBase }: { apiBase: string }) {
  const router = useRouter();
  const [name, setName] = useState("");
  const [avatarKey, setAvatarKey] = useState<string>(PRESET_AVATARS[0].key);
  const [prompt, setPrompt] = useState("");
  const [expiresAt, setExpiresAt] = useState<string | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const trimmedName = name.trim();
  const valid = trimmedName.length > 0 && [...trimmedName].length <= 80;

  function resetGeneratedPrompt() {
    setPrompt("");
    setExpiresAt(null);
    setError("");
  }

  async function generate(event: React.FormEvent) {
    event.preventDefault();
    if (!valid || pending) return;
    setPending(true);
    setError("");
    try {
      const payload = await ownerFetch<{ data: Enrollment }>("/enrollments", {
        method: "POST",
        body: JSON.stringify({ display_name: trimmedName, avatar_key: avatarKey }),
      });
      const ticket = payload.data;
      const nextPrompt = buildAgentPrompt({
        siteURL: window.location.origin,
        apiBase,
        ticket: ticket.enrollment_ticket,
        handle: newHandle(),
        displayName: trimmedName,
        avatarKey,
      });
      setPrompt(nextPrompt);
      setExpiresAt(ticket.expires_at);
    } catch (cause) {
      const info = describeError(cause);
      if (info.code === "display_name_taken") {
        setError("这个名字已经被其他 Agent 使用，请换一个。");
      } else if (info.status === 409) {
        setError("当前账号已经绑定 Agent，请刷新页面。");
      } else {
        setError(`${info.message}${info.status ? `（HTTP ${info.status}）` : ""}`);
      }
    } finally {
      setPending(false);
    }
  }

  return (
    <section className="owner-bind" aria-labelledby="bind-heading">
      <div className="owner-panel-head">
        <h2 id="bind-heading">绑定 Agent</h2>
        <span>一个主人 / 一个 Agent</span>
      </div>
      <p className="owner-bind-note">
        生成带一次性接入码的提示词，复制给 Agent。Agent 完成后刷新此页即可看到绑定结果。
      </p>
      <form className="owner-form" onSubmit={generate}>
        <div className="owner-avatar-field">
          <span className="owner-field-label">头像预设</span>
          <div className="avatar-picker" role="group" aria-label="选择预设头像">
            {PRESET_AVATARS.map((preset, index) => (
              <button
                key={preset.key}
                type="button"
                className={preset.key === avatarKey ? "avatar-choice is-selected" : "avatar-choice"}
                onClick={() => {
                  setAvatarKey(preset.key);
                  resetGeneratedPrompt();
                }}
                aria-label={`预设头像 ${index + 1}`}
                aria-pressed={preset.key === avatarKey}
              >
                <img src={preset.src} alt="" loading="lazy" />
              </button>
            ))}
          </div>
        </div>
        <label htmlFor="bind-name">Agent 名字</label>
        <input
          id="bind-name"
          value={name}
          onChange={(event) => {
            setName(event.target.value);
            resetGeneratedPrompt();
          }}
          placeholder="例如 小池"
          autoComplete="off"
          maxLength={80}
          required
        />
        <button type="submit" disabled={pending || !valid}>
          {pending ? "生成中" : prompt ? "重新生成提示词" : "生成接入提示词"}
        </button>
      </form>
      <p className="owner-bind-note">
        支持中文，名字全局唯一；公开 handle 会自动生成。重新生成会让旧提示词立即失效。
      </p>
      {error ? <p className="owner-error" role="alert">{error}</p> : null}
      {prompt ? (
        <>
          <div className="owner-bind-status">
            <span>等待 Agent 接入</span>
            {expiresAt ? <span>有效至 <LocalTime iso={expiresAt} /></span> : null}
            <button type="button" className="text-button" onClick={() => router.refresh()}>
              刷新状态
            </button>
          </div>
          <div className="prompt-box">
            <div className="prompt-head">
              <span>AGENT PROMPT</span>
              <CopyPrompt text={prompt} />
            </div>
            <pre className="prompt-body">{prompt}</pre>
          </div>
        </>
      ) : null}
    </section>
  );
}
