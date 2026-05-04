import React from "react";
import { View } from "react-native";
import { Link, router } from "expo-router";

import { authClient } from "@/lib/auth";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Text } from "@/components/ui/text";

/**
 * Sample sign-in screen. The AI agent typically replaces this with a real
 * branded screen; the default is here so a fresh template builds + flows
 * end-to-end without manual scaffolding.
 */
export default function SignIn() {
  const [email, setEmail] = React.useState("");
  const [password, setPassword] = React.useState("");
  const [error, setError] = React.useState<string | null>(null);
  const [submitting, setSubmitting] = React.useState(false);

  async function handleSignIn() {
    setError(null);
    setSubmitting(true);
    try {
      const result = await authClient.signIn.email({ email, password });
      if (result.error) {
        setError(result.error.message ?? "Sign-in failed");
      } else {
        router.replace("/(app)");
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Sign-in failed");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <View className="flex-1 items-center justify-center bg-background p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>Welcome back</CardTitle>
        </CardHeader>
        <CardContent>
          <View className="gap-3">
            <Input
              placeholder="Email"
              value={email}
              onChangeText={setEmail}
              autoCapitalize="none"
              autoComplete="email"
              keyboardType="email-address"
            />
            <Input
              placeholder="Password"
              value={password}
              onChangeText={setPassword}
              secureTextEntry
              autoCapitalize="none"
            />
            {error ? (
              <Text variant="muted" className="text-red-500">
                {error}
              </Text>
            ) : null}
            <Button onPress={handleSignIn} disabled={submitting}>
              <Text>{submitting ? "Signing in…" : "Sign in"}</Text>
            </Button>
            <Link href="/(auth)/sign-up" replace>
              <Text variant="muted">Don't have an account? Sign up</Text>
            </Link>
          </View>
        </CardContent>
      </Card>
    </View>
  );
}
