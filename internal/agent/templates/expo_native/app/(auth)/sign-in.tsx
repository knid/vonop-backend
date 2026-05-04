import React from "react";
import { Platform } from "react-native";
import { Link, router } from "expo-router";
import * as AppleAuthentication from "expo-apple-authentication";
import { View } from "@/components/tw";

import {
  authClient,
  signInWithApple,
  signInWithGoogle,
  signInWithMagicLink,
} from "@/lib/auth";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Text } from "@/components/ui/text";

/**
 * Sample sign-in screen. The AI agent typically replaces this with a real
 * branded screen; the default is here so a fresh template builds + flows
 * end-to-end without manual scaffolding.
 *
 * Wires four sign-in paths so adding a third-party provider doesn't
 * inadvertently break Apple Guideline 4.8 (which requires SIWA whenever
 * any social login is offered):
 *   1. Sign in with Apple   (iOS only — system sheet)
 *   2. Continue with Google (browser-based OAuth via Better-Auth)
 *   3. Email magic-link     (one-time link delivered by the backend)
 *   4. Email + password     (Better-Auth's email provider)
 */
export default function SignIn() {
  const [email, setEmail] = React.useState("");
  const [password, setPassword] = React.useState("");
  const [error, setError] = React.useState<string | null>(null);
  const [submitting, setSubmitting] = React.useState(false);
  const [magicLinkEmail, setMagicLinkEmail] = React.useState("");
  const [magicLinkSending, setMagicLinkSending] = React.useState(false);
  const [magicLinkSentTo, setMagicLinkSentTo] = React.useState<string | null>(null);
  const [showMagicLink, setShowMagicLink] = React.useState(false);

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

  async function handleApple() {
    setError(null);
    try {
      await signInWithApple();
      router.replace("/(app)");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  async function handleGoogle() {
    setError(null);
    try {
      await signInWithGoogle();
      router.replace("/(app)");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  async function handleMagicLink() {
    setError(null);
    setMagicLinkSending(true);
    try {
      await signInWithMagicLink(magicLinkEmail);
      setMagicLinkSentTo(magicLinkEmail);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setMagicLinkSending(false);
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
            {Platform.OS === "ios" && (
              <AppleAuthentication.AppleAuthenticationButton
                buttonType={AppleAuthentication.AppleAuthenticationButtonType.SIGN_IN}
                buttonStyle={AppleAuthentication.AppleAuthenticationButtonStyle.BLACK}
                cornerRadius={8}
                style={{ width: "100%", height: 48 }}
                onPress={handleApple}
              />
            )}
            <Button onPress={handleGoogle} variant="outline">
              <Text>Continue with Google</Text>
            </Button>
            <Button onPress={() => setShowMagicLink(true)} variant="outline">
              <Text>Continue with email link</Text>
            </Button>

            <View className="my-2 flex-row items-center gap-2">
              <View className="h-px flex-1 bg-border" />
              <Text variant="muted">or continue with</Text>
              <View className="h-px flex-1 bg-border" />
            </View>

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

      {showMagicLink ? (
        <View
          className="absolute inset-0 items-center justify-center bg-black/40 p-4"
          pointerEvents="auto"
        >
          <Card className="w-full max-w-sm">
            <CardHeader>
              <CardTitle>Sign in with email link</CardTitle>
            </CardHeader>
            <CardContent>
              <View className="gap-3">
                {magicLinkSentTo ? (
                  <Text>
                    Check your inbox at {magicLinkSentTo}. The link will sign you in.
                  </Text>
                ) : (
                  <>
                    <Input
                      placeholder="Email"
                      value={magicLinkEmail}
                      onChangeText={setMagicLinkEmail}
                      autoCapitalize="none"
                      autoComplete="email"
                      keyboardType="email-address"
                    />
                    <Button onPress={handleMagicLink} disabled={magicLinkSending}>
                      <Text>{magicLinkSending ? "Sending…" : "Send link"}</Text>
                    </Button>
                  </>
                )}
                <Button
                  variant="outline"
                  onPress={() => {
                    setShowMagicLink(false);
                    setMagicLinkSentTo(null);
                    setMagicLinkEmail("");
                  }}
                >
                  <Text>Close</Text>
                </Button>
              </View>
            </CardContent>
          </Card>
        </View>
      ) : null}
    </View>
  );
}
