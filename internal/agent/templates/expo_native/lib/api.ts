import { getJwtToken, authClient } from "@/lib/auth";
import { router } from "expo-router";

const API_URL = process.env.EXPO_PUBLIC_API_URL ?? "";

export type ApiError = {
  status: number;
  message: string;
  body?: unknown;
};

/**
 * Typed fetch wrapper for the Wemob backend at {slug}.wemob.app/api/*.
 * Automatically attaches Authorization: Bearer <jwt> if a Better-Auth session exists.
 */
export async function apiFetch<T = unknown>(
  path: string,
  init: RequestInit = {}
): Promise<T> {
  const token = await getJwtToken();
  const headers = new Headers(init.headers);
  headers.set("Content-Type", "application/json");
  if (token) headers.set("Authorization", `Bearer ${token}`);

  const url = `${API_URL}${path.startsWith("/") ? "" : "/"}${path}`;
  const res = await fetch(url, { ...init, headers });

  if (!res.ok) {
    if (res.status === 401) {
      await authClient.signOut();
      router.replace("/(auth)/sign-in");
    }

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
