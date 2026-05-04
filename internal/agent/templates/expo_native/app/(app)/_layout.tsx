import { Redirect, Stack } from "expo-router";
import Constants from "expo-constants";
import { authClient } from "@/lib/auth";

/**
 * Auth-guarded route group. All screens nested under app/(app)/ require
 * a Better-Auth session; users without one are redirected to /sign-in.
 *
 * The (auth) sibling group (app/(auth)/sign-in.tsx + sign-up.tsx) is the
 * public counterpart — accessible without a session.
 *
 * When MobileAppConfig.AuthEnabled is false on the backend, the scaffold
 * sets `extra.authEnabled` in app.json to false and skips copying the
 * (auth) route group entirely. The guard below becomes a no-op so the app
 * boots straight into the main stack.
 */
const AUTH_ENABLED = Constants.expoConfig?.extra?.authEnabled === true;

export default function AppLayout() {
  const { data: session, isPending } = authClient.useSession();

  if (AUTH_ENABLED) {
    if (isPending) return null;
    if (!session) return <Redirect href="/(auth)/sign-in" />;
  }

  return <Stack screenOptions={{ headerShown: false }} />;
}
