import { Redirect, Stack } from "expo-router";
import { authClient } from "@/lib/auth";

/**
 * Auth-guarded route group. All screens nested under app/(app)/ require
 * a Better-Auth session; users without one are redirected to /sign-in.
 *
 * The (auth) sibling group (app/(auth)/sign-in.tsx + sign-up.tsx) is the
 * public counterpart — accessible without a session.
 *
 * When MobileAppConfig.AuthEnabled is false on the backend, the auth client
 * still works locally (useSession returns null + isPending settles to false
 * quickly), so the user is sent to /sign-in. The AI agent that generates
 * actual app code is responsible for either disabling this guard (delete
 * this file) or wiring backend/lib/auth.ts so sign-in actually works.
 */
export default function AppLayout() {
  const { data: session, isPending } = authClient.useSession();

  if (isPending) return null;

  if (!session) return <Redirect href="/(auth)/sign-in" />;

  return <Stack screenOptions={{ headerShown: false }} />;
}
