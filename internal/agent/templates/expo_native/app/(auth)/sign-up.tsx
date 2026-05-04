import React from "react";
import { View } from "react-native";
import { Link, router } from "expo-router";

import { authClient } from "@/lib/auth";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Text } from "@/components/ui/text";

/**
 * Sample sign-up screen. Pairs with sign-in.tsx — the AI agent usually
 * replaces both with branded copies. The shipped default uses Better-Auth's
 * email/password provider, which is the only thing P2.4 enables out of the
 * box; future provider plumbing (google/apple/github) extends this screen.
 */
export default function SignUp() {
  const [name, setName] = React.useState("");
  const [email, setEmail] = React.useState("");
  const [password, setPassword] = React.useState("");
  const [error, setError] = React.useState<string | null>(null);
  const [submitting, setSubmitting] = React.useState(false);

  async function handleSignUp() {
    setError(null);
    setSubmitting(true);
    try {
      const result = await authClient.signUp.email({ name, email, password });
      if (result.error) {
        setError(result.error.message ?? "Sign-up failed");
      } else {
        router.replace("/(app)");
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Sign-up failed");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <View className="flex-1 items-center justify-center bg-background p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>Create account</CardTitle>
        </CardHeader>
        <CardContent>
          <View className="gap-3">
            <Input
              placeholder="Name"
              value={name}
              onChangeText={setName}
              autoCapitalize="words"
              autoComplete="name"
            />
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
            <Button onPress={handleSignUp} disabled={submitting}>
              <Text>{submitting ? "Creating…" : "Create account"}</Text>
            </Button>
            <Link href="/(auth)/sign-in" replace>
              <Text variant="muted">Already have an account? Sign in</Text>
            </Link>
          </View>
        </CardContent>
      </Card>
    </View>
  );
}
