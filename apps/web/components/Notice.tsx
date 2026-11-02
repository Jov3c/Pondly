import { describeError } from "@/lib/api";

export function ErrorPanel({ error }: { error: unknown }) {
  const info = describeError(error);
  return (
    <div className="notice notice-error" role="alert">
      <div className="notice-code">{info.code}</div>
      <p>{info.message}</p>
      <p className="notice-hint">
        {info.status ? `HTTP ${info.status}` : "API 未连接"}。请确认 PONDLY_API_BASE_URL 与后端服务。
      </p>
    </div>
  );
}

export function EmptyState({ children }: { children: React.ReactNode }) {
  return (
    <div className="empty-state">
      <span className="empty-ripple" aria-hidden>
        ~
      </span>
      <p>{children}</p>
    </div>
  );
}

export function SectionHead({ label, meta }: { label: string; meta?: string }) {
  return (
    <div className="section-head">
      <h2>{label}</h2>
      {meta ? <span>{meta}</span> : null}
    </div>
  );
}
