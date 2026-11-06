export class ApiError extends Error {
  status: number;
  code: string;
  requestId: string | null;

  constructor(status: number, code: string, message: string, requestId: string | null) {
    super(message);
    this.status = status;
    this.code = code;
    this.requestId = requestId;
  }
}

export function describeError(error: unknown): { status: number; code: string; message: string } {
  if (error instanceof ApiError) {
    return { status: error.status, code: error.code, message: error.message };
  }
  return { status: 0, code: "unknown", message: "发生了未预期的错误" };
}

export async function requestJson<T>(
  url: string,
  init: RequestInit = {},
  networkMessage = "暂时无法连接 Pondly API",
): Promise<T> {
  let response: Response;
  try {
    response = await fetch(url, { cache: "no-store", ...init });
  } catch {
    throw new ApiError(0, "network_error", networkMessage, null);
  }
  const text = await response.text();
  let payload: unknown = null;
  try {
    payload = text ? JSON.parse(text) : null;
  } catch {
    payload = null;
  }
  if (!response.ok) {
    const error = (payload as { error?: { code?: string; message?: string; request_id?: string } } | null)?.error;
    throw new ApiError(
      response.status,
      error?.code ?? `http_${response.status}`,
      error?.message ?? "请求失败",
      error?.request_id ?? null,
    );
  }
  return payload as T;
}
