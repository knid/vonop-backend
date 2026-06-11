import { Redirect, Stack } from "expo-router";
import { authClient } from "@/lib/auth";

/**
 * Public route group: sign-in / sign-up. Already-authenticated users are
 * bounced to the protected (app) group so they don't see the sign-in screen
 * unnecessarily.
 */
export default function AuthLayout() {
  const { data: session, isPending } = authClient.useSession();

  if (isPending) return null;

  if (session) return <Redirect href="/(app)" />;

  return <Stack screenOptions={{ headerShown: false }} />;
}
