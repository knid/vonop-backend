import React from "react";
import { Platform, View } from "react-native";
import { Link, router } from "expo-router";

import {
  authClient,
  signInWithApple,
  signInWithGoogle,
  signInWithMagicLink,
} from "@/lib/auth";

let AppleAuthentication: typeof import("expo-apple-authentication") | null = null;
try {
  AppleAuthentication = require("expo-apple-authentication");
} catch {
  // Not installed
}
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Text } from "@/components/ui/text";

type AuthFormProps = {
  mode: "sign-in" | "sign-up";
  onSubmit?: (values: {
    email: string;
    password: string;
    name?: string;
  }) => Promise<void>;
  isLoading?: boolean;
};

export default function AuthForm({
  mode,
  onSubmit,
  isLoading: externalLoading,
}: AuthFormProps) {
  const isSignIn = mode === "sign-in";

  const [name, setName] = React.useState("");
  const [email, setEmail] = React.useState("");
  const [password, setPassword] = React.useState("");
  const [error, setError] = React.useState<string | null>(null);
  const [submitting, setSubmitting] = React.useState(false);
  const [magicLinkEmail, setMagicLinkEmail] = React.useState("");
  const [magicLinkSending, setMagicLinkSending] = React.useState(false);
  const [magicLinkSentTo, setMagicLinkSentTo] = React.useState<string | null>(
    null
  );
  const [showMagicLink, setShowMagicLink] = React.useState(false);

  const isLoading = externalLoading ?? submitting;

  async function handleEmailSubmit() {
    setError(null);
    setSubmitting(true);
    try {
      if (onSubmit) {
        await onSubmit({
          email,
          password,
          ...(isSignIn ? {} : { name }),
        });
      } else {
        if (isSignIn) {
          const result = await authClient.signIn.email({ email, password });
          if (result.error) {
            setError(result.error.message ?? "Sign-in failed");
            return;
          }
        } else {
          const result = await authClient.signUp.email({
            name,
            email,
            password,
          });
          if (result.error) {
            setError(result.error.message ?? "Sign-up failed");
            return;
          }
        }
      }
      router.replace("/(app)");
    } catch (e) {
      setError(
        e instanceof Error
          ? e.message
          : isSignIn
            ? "Sign-in failed"
            : "Sign-up failed"
      );
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
          <CardTitle>
            {isSignIn ? "Welcome back" : "Create account"}
          </CardTitle>
        </CardHeader>
        <CardContent>
          <View className="gap-3">
            {Platform.OS === "ios" && AppleAuthentication && (
              <AppleAuthentication.AppleAuthenticationButton
                buttonType={
                  isSignIn
                    ? AppleAuthentication.AppleAuthenticationButtonType.SIGN_IN
                    : AppleAuthentication.AppleAuthenticationButtonType.SIGN_UP
                }
                buttonStyle={
                  AppleAuthentication.AppleAuthenticationButtonStyle.BLACK
                }
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
              <Text variant="muted">
                {isSignIn ? "or continue with" : "or sign up with"}
              </Text>
              <View className="h-px flex-1 bg-border" />
            </View>

            {!isSignIn && (
              <Input
                placeholder="Name"
                value={name}
                onChangeText={setName}
                autoCapitalize="words"
                autoComplete="name"
              />
            )}
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
            <Button onPress={handleEmailSubmit} disabled={isLoading}>
              <Text>
                {isLoading
                  ? isSignIn
                    ? "Signing in…"
                    : "Creating…"
                  : isSignIn
                    ? "Sign in"
                    : "Create account"}
              </Text>
            </Button>
            <Link
              href={isSignIn ? "/(auth)/sign-up" : "/(auth)/sign-in"}
              replace
            >
              <Text variant="muted">
                {isSignIn
                  ? "Don't have an account? Sign up"
                  : "Already have an account? Sign in"}
              </Text>
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
              <CardTitle>
                {isSignIn
                  ? "Sign in with email link"
                  : "Sign up with email link"}
              </CardTitle>
            </CardHeader>
            <CardContent>
              <View className="gap-3">
                {magicLinkSentTo ? (
                  <Text>
                    Check your inbox at {magicLinkSentTo}. The link will sign
                    you in.
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
                    <Button
                      onPress={handleMagicLink}
                      disabled={magicLinkSending}
                    >
                      <Text>
                        {magicLinkSending ? "Sending…" : "Send link"}
                      </Text>
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
