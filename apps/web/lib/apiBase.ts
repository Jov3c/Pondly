const DEFAULT_API_BASE = "http://127.0.0.1:8080";

// .env.example ships the public key empty, so unset and blank must both fall
// back: a blank base would put relative URLs into the Agent prompt.
export const API_BASE = (process.env.PONDLY_API_BASE_URL || DEFAULT_API_BASE).replace(/\/$/, "");

export const PUBLIC_API_BASE = (process.env.PONDLY_PUBLIC_API_BASE_URL || API_BASE).replace(/\/$/, "");
