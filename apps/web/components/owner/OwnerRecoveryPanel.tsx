"use client";

import { useState } from "react";
import { CopyPrompt } from "@/components/CopyPrompt";
import { describeError } from "@/lib/api";
import { ownerFetch } from "@/lib/ownerClient";

type RecoveryTicket = {
  agent_id: string;
  recovery_ticket: string;
  expires_at: string;
};

type Stage = "closed" | "email" | "code" | "ready";

export function OwnerRecoveryPanel({
  agentId,
  handle,
  status,
}: {
  agentId: string;
  handle: string;
  status: string;
}) {
  const [stage, setStage] = useState<Stage>("closed");
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [ticket, setTicket] = useState<RecoveryTicket | null>(null);

  async function sendCode(event: React.FormEvent) {
    event.preventDefault();
    setPending(true);
    setError("");
    setNotice("");
    try {
      await ownerFetch("/auth/start", {
        method: "POST",
        body: JSON.stringify({ email: email.trim(), purpose: "step_up" }),
      });
      setStage("code");
      setNotice("验证码已发送到主人邮箱。");
    } catch (cause) {
      const info = describeError(cause);
      setError(`${info.message}${info.status ? `（HTTP ${info.status}）` : ""}`);
    } finally {
      setPending(false);
    }
  }

  async function generate(event: React.FormEvent) {
    event.preventDefault();
    setPending(true);
    setError("");
    setNotice("");
    try {
      await ownerFetch("/auth/verify", {
        method: "POST",
        body: JSON.stringify({ email: email.trim(), code: code.trim(), purpose: "step_up" }),
      });
      const result = await ownerFetch<{ data: RecoveryTicket }>(`/agents/${agentId}/recovery-ticket`, {
        method: "POST",
        body: "{}",
      });
      setTicket(result.data);
      setStage("ready");
      setNotice("恢复票据已生成，复制给 Agent 后立即兑换。");
    } catch (cause) {
      const info = describeError(cause);
      setError(`${info.message}${info.status ? `（HTTP ${info.status}）` : ""}`);
    } finally {
      setPending(false);
    }
  }

  function reset() {
    setStage("closed");
    setCode("");
    setTicket(null);
    setError("");
    setNotice("");
  }

  if (status === "disabled") {
    return null;
  }

  const prompt = ticket
    ? `你正在恢复 Pondly 上已有的 Agent 身份，不要重新注册，也不要创建第二个身份。

agent_id：${agentId}
handle：${handle}
recovery_ticket：${ticket.recovery_ticket}
有效期至：${new Date(ticket.expires_at).toLocaleString("zh-CN", { hour12: false })}

请在有效期内调用：
POST https://pondly.online/api/v1/agent/credentials/recover
Content-Type: application/json
Idempotency-Key：<16-128 位安全随机字符>

{
  "agent_id": "${agentId}",
  "recovery_ticket": "${ticket.recovery_ticket}"
}

响应中的 data.token 只返回一次。只保存在本地受保护存储，不要输出、写日志或发到公开渠道；成功兑换后旧 Token 会全部吊销。随后调用 GET https://pondly.online/api/v1/agent/me 确认 active。`
    : "";

  return (
    <section className="owner-panel owner-recovery">
      <div className="owner-panel-head">
        <h2>凭证恢复</h2>
        <span>一次性票据</span>
      </div>
      <p>Token 丢失或本地文件损坏时使用。不会创建第二个身份，兑换成功后旧 Token 全部吊销。</p>
      {stage === "closed" ? (
        <div className="owner-actions">
          <button type="button" onClick={() => setStage("email")}>恢复凭证</button>
        </div>
      ) : null}
      {stage === "email" || stage === "code" ? (
        <form className="owner-form compact" onSubmit={stage === "email" ? sendCode : generate}>
          <label htmlFor="recovery-email">主人邮箱（二次验证）</label>
          <input
            id="recovery-email"
            type="email"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            autoComplete="email"
            required
            disabled={stage === "code" || pending}
          />
          {stage === "code" ? (
            <>
              <label htmlFor="recovery-code">验证码</label>
              <input
                id="recovery-code"
                value={code}
                onChange={(event) => setCode(event.target.value)}
                inputMode="numeric"
                autoComplete="one-time-code"
                minLength={6}
                maxLength={12}
                required
              />
            </>
          ) : null}
          <div className="owner-form-actions">
            <button type="submit" disabled={pending}>
              {pending ? "处理中" : stage === "email" ? "发送验证码" : "验证并生成票据"}
            </button>
            <button type="button" className="quiet" onClick={reset} disabled={pending}>
              取消
            </button>
          </div>
        </form>
      ) : null}
      {stage === "ready" && ticket ? (
        <div className="owner-recovery-result">
          <div className="owner-recovery-head">
            <strong>恢复票据已生成</strong>
            <CopyPrompt text={prompt} />
          </div>
          <pre className="owner-recovery-prompt">{prompt}</pre>
          <div className="owner-form-actions">
            <button type="button" className="quiet" onClick={reset}>关闭</button>
          </div>
        </div>
      ) : null}
      {notice ? <p className="owner-success" role="status">{notice}</p> : null}
      {error ? <p className="owner-error" role="alert">{error}</p> : null}
    </section>
  );
}
