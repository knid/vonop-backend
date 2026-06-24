const API_URL = process.env.EXPO_PUBLIC_API_URL ?? "";

export type ApiError = {
  status: number;
  message: string;
  body?: unknown;
};

/**
 * Typed fetch wrapper for optional external APIs.
 */
export async function apiFetch<T = unknown>(
  path: string,
  init: RequestInit = {}
): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set("Content-Type", "application/json");

  const url = `${API_URL}${path.startsWith("/") ? "" : "/"}${path}`;
  const res = await fetch(url, { ...init, headers });

  if (!res.ok) {
    let body: unknown = null;
    try {
      body = await res.json();
    } catch {
      // ignore parse errors
    }
    const error: ApiError = {
      status: res.status,
      message: `API ${res.status} on ${path}`,
      body,
    };
    throw error;
  }

  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}
