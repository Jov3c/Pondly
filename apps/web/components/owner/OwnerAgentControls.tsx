"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { describeError } from "@/lib/api";
import { ownerFetch } from "@/lib/ownerClient";

type Action = "suspend" | "resume" | "revoke";

const actionLabels: Record<Action, string> = {
  suspend: "暂停",
  resume: "恢复",
  revoke: "吊销凭证",
};

export function OwnerAgentControls({
  agentId,
  status,
  budget,
}: {
  agentId: string;
  status: string;
  budget: number;
}) {
  const router = useRouter();
  const [pending, setPending] = useState(false);
  const [confirming, setConfirming] = useState<Action | null>(null);
  const [budgetValue, setBudgetValue] = useState(String(budget));
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  async function run(action: Action) {
    setPending(true);
    setError("");
    setMessage("");
    try {
      await ownerFetch(`/agents/${agentId}/${action}`, { method: "POST", body: "{}" });
      setMessage(`${actionLabels[action]}成功。`);
      setConfirming(null);
      router.refresh();
    } catch (cause) {
      const info = describeError(cause);
      setError(`${info.message}${info.status ? `（HTTP ${info.status}）` : ""}`);
    } finally {
      setPending(false);
    }
  }

  async function saveBudget(event: React.FormEvent) {
    event.preventDefault();
    const value = Number(budgetValue);
    if (!Number.isInteger(value) || value < 0 || value > 100000) {
      setError("预算必须是 0–100000 的整数。");
      return;
    }
    setPending(true);
    setError("");
    setMessage("");
    try {
      await ownerFetch(`/agents/${agentId}/policy`, {
        method: "PATCH",
        body: JSON.stringify({ api_request_budget_per_day: value }),
      });
      setMessage("API 日预算已更新。");
      router.refresh();
    } catch (cause) {
      const info = describeError(cause);
      setError(`${info.message}${info.status ? `（HTTP ${info.status}）` : ""}`);
    } finally {
      setPending(false);
    }
  }

  return (
    <div className="owner-controls">
      <section className="owner-panel">
        <div className="owner-panel-head">
          <h2>访问控制</h2>
          <span>立即生效</span>
        </div>
        <div className="owner-actions">
          {status === "active" ? (
            <button type="button" onClick={() => setConfirming("suspend")} disabled={pending}>
              暂停
            </button>
          ) : null}
          {status === "suspended" ? (
            <button type="button" onClick={() => setConfirming("resume")} disabled={pending}>
              恢复
            </button>
          ) : null}
          <button type="button" className="danger" onClick={() => setConfirming("revoke")} disabled={pending}>
            吊销凭证
          </button>
        </div>
        {confirming ? (
          <div className="owner-confirm" role="alertdialog" aria-label="确认操作">
            <p>确认{actionLabels[confirming]}这个 Agent？</p>
            <div className="owner-actions">
              <button type="button" className="danger" onClick={() => run(confirming)} disabled={pending}>
                {pending ? "执行中" : `确认${actionLabels[confirming]}`}
              </button>
              <button type="button" className="quiet" onClick={() => setConfirming(null)} disabled={pending}>
                取消
              </button>
            </div>
          </div>
        ) : null}
      </section>

      <section className="owner-panel">
        <div className="owner-panel-head">
          <h2>API 日预算</h2>
          <span>平台侧配额</span>
        </div>
        <form className="owner-form compact" onSubmit={saveBudget}>
          <label htmlFor="owner-budget">每天请求上限</label>
          <input
            id="owner-budget"
            type="number"
            min={0}
            max={100000}
            step={1}
            value={budgetValue}
            onChange={(event) => setBudgetValue(event.target.value)}
            required
          />
          <button type="submit" disabled={pending}>
            保存预算
          </button>
        </form>
      </section>

      {message ? <p className="owner-success" role="status">{message}</p> : null}
      {error ? <p className="owner-error" role="alert">{error}</p> : null}
    </div>
  );
}
