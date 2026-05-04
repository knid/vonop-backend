import { expoClient } from "@better-auth/expo/client";
import { magicLinkClient } from "better-auth/client/plugins";
import { createAuthClient } from "better-auth/react";
import * as AppleAuthentication from "expo-apple-authentication";
import Constants from "expo-constants";
import * as SecureStore from "expo-secure-store";
import * as WebBrowser from "expo-web-browser";

// Ensure any in-flight web auth session is dismissed when the app rehydrates,
// e.g. after returning from a Google OAuth redirect. Safe no-op on native if
// no session is active.
WebBrowser.maybeCompleteAuthSession();

// EXPO_PUBLIC_AUTH_BASE_URL is baked at build time by the codemagic pipeline
// when MobileAppConfig.AuthEnabled is true on the backend. Falls back to the
// general API URL so disabled-auth builds still typecheck (and any agent code
// that calls authClient against a non-auth backend gets a 404, which is
// recoverable + obvious).
const AUTH_BASE_URL =
  process.env.EXPO_PUBLIC_AUTH_BASE_URL ?? process.env.EXPO_PUBLIC_API_URL ?? "";

// Per-app URL scheme. The codemagic build pipeline rewrites app.json's
// expo.scheme to the project slug at build time, and the same slug is wired
// into the backend's trustedOrigins via the agent prompt's auth wiring
// guidance. We read it at runtime from Constants.expoConfig (sourced from
// the substituted app.json) so OAuth/SIWA redirects round-trip correctly
// per-customer without needing a second substitution pass over lib/.
const SCHEME = Constants.expoConfig?.scheme;
const RESOLVED_SCHEME = Array.isArray(SCHEME) ? SCHEME[0] : SCHEME;
if (!RESOLVED_SCHEME) {
  throw new Error(
    "expo.scheme missing in app.json — auth deeplinks will not work",
  );
}
export const authClient = createAuthClient({
  baseURL: AUTH_BASE_URL,
  plugins: [
    expoClient({
      scheme: RESOLVED_SCHEME,
      storagePrefix: "wemob_auth",
      storage: SecureStore,
    }),
    // Enables `authClient.signIn.magicLink({ email })`. Requires the
    // backend's Better-Auth instance to register the `magicLink({...})`
    // server plugin AND a sendMagicLink callback that delivers the email
    // (via Resend, Brevo, Postmark, etc.).
    magicLinkClient(),
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

// ---------------------------------------------------------------------------
// Social + magic-link helpers
// ---------------------------------------------------------------------------
//
// Apple Guideline 4.8 REQUIRES Sign in with Apple whenever any third-party
// social login (Google, Facebook, etc.) is offered. The template ships
// SIWA wired by default so adding Google etc. doesn't break review.
//
// All three helpers throw on failure; UI screens should try/catch and surface
// `error.message` to the user.

/**
 * Native Sign in with Apple (iOS only). Triggers Apple's system sheet,
 * receives a signed identity token, and forwards it to Better-Auth's
 * `apple` social provider via the native ID-token path (no browser hop).
 *
 * Backend requirement: `socialProviders.apple = { clientId, ... }` in the
 * Better-Auth server config. With ID-token verification enabled, the
 * server validates Apple's signed JWT against Apple's JWKS and creates
 * (or links) a session user.
 */
export async function signInWithApple() {
  const credential = await AppleAuthentication.signInAsync({
    requestedScopes: [
      AppleAuthentication.AppleAuthenticationScope.FULL_NAME,
      AppleAuthentication.AppleAuthenticationScope.EMAIL,
    ],
  });
  if (!credential.identityToken) {
    throw new Error("Apple did not return an identity token");
  }
  // Apple's SIWA flow does not provide a separate nonce; the authorizationCode
  // is single-use server-side. Better-Auth's apple provider only requires the
  // identity token — pass nonce only if the server config sets a nonce policy.
  const result = await authClient.signIn.social({
    provider: "apple",
    idToken: {
      token: credential.identityToken,
    },
  });
  if (result.error) throw new Error(result.error.message ?? "Apple sign-in failed");
  return result.data;
}

/**
 * Google sign-in via Better-Auth's social-redirect flow. The
 * `@better-auth/expo` client plugin transparently opens
 * `WebBrowser.openAuthSessionAsync`, completes the OAuth round-trip, and
 * persists the resulting session cookie.
 *
 * Backend requirement: `socialProviders.google = { clientId, clientSecret }`
 * with the iOS bundle id and Android package name registered in the
 * Google Cloud OAuth consent screen.
 */
export async function signInWithGoogle() {
  const result = await authClient.signIn.social({
    provider: "google",
    // Relative path → @better-auth/expo turns this into a Linking.createURL
    // deep link using `expo.scheme`, which round-trips back into the app.
    callbackURL: "/",
  });
  if (result.error) throw new Error(result.error.message ?? "Google sign-in failed");
  return result.data;
}

/**
 * Email magic-link. Sends a one-time sign-in link to the user; the
 * backend's `sendMagicLink` callback is responsible for delivering the
 * email. The link's verify URL deep-links back into the app via the
 * configured scheme.
 *
 * Backend requirement: `magicLink({ sendMagicLink: ... })` plugin in the
 * Better-Auth server config.
 */
export async function signInWithMagicLink(email: string) {
  const result = await authClient.signIn.magicLink({ email });
  if (result.error) throw new Error(result.error.message ?? "Could not send magic link");
  return result.data;
}
