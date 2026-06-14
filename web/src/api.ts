import type { LinkDraft, LinkResponse, Stats } from "./types";

const API_KEY_STORAGE = "deeplink.apiKey";

// One env var (VITE_API_URL) describes the Go service URL. In dev,
// fetches stay relative so Vite's proxy forwards them — no CORS, no
// scheme footgun. In production builds the absolute URL is baked in.
function normalizeURL(raw: string): string {
  const v = raw.trim().replace(/\/$/, "");
  if (!v) return "";
  if (/^https?:\/\//i.test(v)) return v;
  console.warn(`VITE_API_URL "${v}" missing scheme; assuming http://`);
  return `http://${v}`;
}

const API_BASE_URL = import.meta.env.DEV
  ? ""
  : normalizeURL(import.meta.env.VITE_API_URL ?? "");

export function getApiKey(): string {
  return localStorage.getItem(API_KEY_STORAGE) ?? "";
}

export function setApiKey(key: string): void {
  if (key) {
    localStorage.setItem(API_KEY_STORAGE, key);
  } else {
    localStorage.removeItem(API_KEY_STORAGE);
  }
}

function authHeaders(): Record<string, string> {
  const key = getApiKey();
  if (!key) return {};
  return { "X-API-Key": key };
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${API_BASE_URL}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...authHeaders(),
      ...init?.headers,
    },
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `${response.status} ${response.statusText}`);
  }
  if (response.status === 204) {
    return undefined as T;
  }
  return (await response.json()) as T;
}

export function listLinks(): Promise<LinkResponse[]> {
  return request<LinkResponse[]>("/links");
}

export function listTypes(): Promise<string[]> {
  return request<string[]>("/types");
}

export function getStats(shortID: string): Promise<Stats> {
  return request<Stats>(`/stats/${shortID}`);
}

export function createLink(draft: LinkDraft): Promise<{ short_url: string }> {
  return request<{ short_url: string }>("/shorten", {
    method: "POST",
    body: JSON.stringify(draft),
  });
}

export function updateLink(shortID: string, patch: Partial<LinkDraft>): Promise<LinkResponse> {
  return request<LinkResponse>(`/${shortID}`, {
    method: "PATCH",
    body: JSON.stringify(patch),
  });
}

export function deleteLink(shortID: string): Promise<void> {
  return request<void>(`/${shortID}`, { method: "DELETE" });
}
