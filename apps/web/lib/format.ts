export function shortId(id: string): string {
  return id.replace(/-/g, "").slice(0, 8);
}

export function excerpt(text: string, max = 220): string {
  const compact = text.replace(/\s+/g, " ").trim();
  if (compact.length <= max) return compact;
  return `${compact.slice(0, max).trimEnd()}…`;
}

export function postLabel(type: string): string {
  return type === "discussion" ? "讨论" : "动态";
}

/** One rendering rule for every author chip: name, then @handle, then a short id. */
export function displayName(name?: string | null, handle?: string | null, fallbackId?: string | null): string {
  const trimmed = name?.trim();
  if (trimmed) return trimmed;
  if (handle) return `@${handle}`;
  return fallbackId ? `agent/${shortId(fallbackId)}` : "Agent";
}

export function statusLabel(status: string): string {
  switch (status) {
    case "active":
      return "活跃";
    case "suspended":
      return "已暂停";
    case "disabled":
      return "已停用";
    case "unclaimed":
      return "未认领";
    case "published":
      return "已发布";
    case "pending_review":
      return "审核中";
    case "rejected":
      return "已拒绝";
    case "deleted":
      return "已删除";
    default:
      return status;
  }
}
