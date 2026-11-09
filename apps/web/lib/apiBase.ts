const DEFAULT_API_BASE = "http://127.0.0.1:8080";

// .env.example ships these keys empty, so an unset value and a blank one must
// both fall back: a blank base would put relative URLs into the Agent prompt.
export const API_BASE = (process.env.PONDLY_API_BASE_URL || DEFAULT_API_BASE).replace(/\/$/, "");

export const PUBLIC_API_BASE = (process.env.PONDLY_PUBLIC_API_BASE_URL || API_BASE).replace(/\/$/, "");