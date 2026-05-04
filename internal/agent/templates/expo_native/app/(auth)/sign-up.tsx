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
 * Sample sign-up screen. Pairs with sign-in.tsx — the AI agent usually
 * replaces both with branded copies. The shipped default uses Better-Auth's
 * email/password provider plus social helpers (Apple/Google/magic-link).
 *
 * SIWA is mandatory whenever any third-party social login is offered
 * (Apple Guideline 4.8); keep the SIWA button on this screen even when
 * customising the rest of the UX.
 */
export default function SignUp() {
  const [name, setName] = React.useState("");
  const [email, setEmail] = React.useState("");
  const [password, setPassword] = React.useState("");
  const [error, setError] = React.useState<string | null>(null);
  const [submitting, setSubmitting] = React.useState(false);
  const [magicLinkEmail, setMagicLinkEmail] = React.useState("");
  const [magicLinkSending, setMagicLinkSending] = React.useState(false);
  const [magicLinkSentTo, setMagicLinkSentTo] = React.useState<string | null>(null);
  const [showMagicLink, setShowMagicLink] = React.useState(false);

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
          <CardTitle>Create account</CardTitle>
        </CardHeader>
        <CardContent>
          <View className="gap-3">
            {Platform.OS === "ios" && (
              <AppleAuthentication.AppleAuthenticationButton
                buttonType={AppleAuthentication.AppleAuthenticationButtonType.SIGN_UP}
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
              <Text variant="muted">or sign up with</Text>
              <View className="h-px flex-1 bg-border" />
            </View>

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

      {showMagicLink ? (
        <View
          className="absolute inset-0 items-center justify-center bg-black/40 p-4"
          pointerEvents="auto"
        >
          <Card className="w-full max-w-sm">
            <CardHeader>
              <CardTitle>Sign up with email link</CardTitle>
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
