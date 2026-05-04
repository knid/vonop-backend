import { expoClient } from "@better-auth/expo/client";
import { createAuthClient } from "better-auth/react";
import * as SecureStore from "expo-secure-store";

// EXPO_PUBLIC_AUTH_BASE_URL is baked at build time by the codemagic pipeline
// when MobileAppConfig.AuthEnabled is true on the backend. Falls back to the
// general API URL so disabled-auth builds still typecheck (and any agent code
// that calls authClient against a non-auth backend gets a 404, which is
// recoverable + obvious).
const AUTH_BASE_URL =
  process.env.EXPO_PUBLIC_AUTH_BASE_URL ?? process.env.EXPO_PUBLIC_API_URL ?? "";

// Per-app URL scheme. The codemagic build pipeline rewrites app.json's
// expo.scheme to the project slug, and the same slug is wired into the
// backend's trustedOrigins via the agent prompt's auth wiring guidance.
// Keeping the scheme here in sync via the same __APP_SLUG__ placeholder
// means OAuth redirects round-trip correctly per-customer.
export const authClient = createAuthClient({
  baseURL: AUTH_BASE_URL,
  plugins: [
    expoClient({
      scheme: "__APP_SLUG__",
      storagePrefix: "__APP_SLUG__",
      storage: SecureStore,
    }),
  ],
});

/**
 * Returns the session token to send in Authorization: Bearer headers to our backend.
 *
 * Note: this returns the Better-Auth session token (not a JWT). The backend MUST enable
 * Better-Auth's `bearer()` plugin so it accepts session tokens via Authorization header.
 * If/when the backend enables the JWT plugin, swap to `authClient.token()` here for true JWTs.
 */
export async function getJwtToken(): Promise<string | null> {
  try {
    const result = await authClient.getSession();
    if (result.error || !result.data?.session?.token) return null;
    return result.data.session.token;
  } catch {
    return null;
  }
}
