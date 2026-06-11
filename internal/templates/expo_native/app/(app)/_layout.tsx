import { Stack } from "expo-router";

/**
 * App route group layout. Renders the screen stack without headers.
 *
 * When the user asks for authentication, the AI agent should add an
 * auth guard here (Redirect to /(auth)/sign-in when no session) and
 * create the (auth) route group with sign-in / sign-up screens.
 */
export default function AppLayout() {
  return <Stack screenOptions={{ headerShown: false }} />;
}
