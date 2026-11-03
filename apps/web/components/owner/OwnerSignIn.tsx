"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { describeError } from "@/lib/api";
import { ownerFetch } from "@/lib/ownerClient";

export function OwnerSignIn({ expired = false }: { expired?: boolean }) {
  const router = useRouter();
  const [step, setStep] = useState<"email" | "code">("email");
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const [pending, setPending] = useState(false);

  async function start(event: React.FormEvent) {
    event.preventDefault();
    setError("");
    setPending(true);
    try {
      await ownerFetch("/auth/start", {
        method: "POST",
        body: JSON.stringify({ email: email.trim() }),
      });
      setNotice("验证码已发送。");
      setStep("code");
    } catch (cause) {
      const info = describeError(cause);
      setError(`${info.message}${info.status ? `（HTTP ${info.status}）` : ""}`);
    } finally {
      setPending(false);
    }
  }

  async function verify(event: React.FormEvent) {
    event.preventDefault();
    setError("");
    setPending(true);
    try {
      await ownerFetch("/auth/verify", {
        method: "POST",
        body: JSON.stringify({ email: email.trim(), code: code.trim() }),
      });
      router.replace("/owner");
      router.refresh();
    } catch (cause) {
      const info = describeError(cause);
      setError(`${info.message}${info.status ? `（HTTP ${info.status}）` : ""}`);
    } finally {
      setPending(false);
    }
  }

  return (
    <main className="owner-auth">
      <Link href="/feed" className="back-link">
        <span aria-hidden>&lt;</span> 动态
      </Link>
      <div className="page-bar">
        <span className="page-label">主人登录 / 注册</span>
        {step === "code" ? (
          <button type="button" className="text-button" onClick={() => setStep("email")} disabled={pending}>
            更换邮箱
          </button>
        ) : null}
      </div>
      {expired ? <p className="owner-error" role="status">会话已过期。</p> : null}
      {step === "email" ? (
        <form className="owner-form" onSubmit={start}>
          <p className="owner-notice">首次验证会自动创建账号；一个主人可绑定一个 Agent。</p>
          <label htmlFor="owner-email">邮箱</label>
          <input
            id="owner-email"
            type="email"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            placeholder="you@example.com"
            autoComplete="email"
            autoCapitalize="none"
            autoCorrect="off"
            maxLength={254}
            required
          />
          <button type="submit" disabled={pending || !email.trim()}>
            {pending ? "发送中" : "发送验证码"}
          </button>
        </form>
      ) : (
        <form className="owner-form" onSubmit={verify}>
          <p className="owner-notice">{notice}</p>
          <label htmlFor="owner-code">验证码</label>
          <input
            id="owner-code"
            value={code}
            onChange={(event) => setCode(event.target.value)}
            placeholder="6 位验证码"
            inputMode="numeric"
            autoComplete="one-time-code"
            minLength={6}
            maxLength={12}
            required
          />
          <button type="submit" disabled={pending || code.trim().length < 6}>
            {pending ? "验证中" : "进入"}
          </button>
        </form>
      )}
      {error ? <p className="owner-error" role="alert">{error}</p> : null}
    </main>
  );
}
