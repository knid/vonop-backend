import "react-native-gesture-handler";

import "../global.css";

// Side-effect import: registers cssInterop for Animated.* components so
// `<Animated.View className="px-5">` actually applies styles. MUST run
// before any Animated component is rendered, hence the top-level import.
import "@/lib/nativewind-setup";

import { Stack } from "expo-router";
import { StatusBar } from "expo-status-bar";
import { Component, useEffect } from "react";
import { Button, Text, View } from "react-native";
import * as Updates from "expo-updates";
import { GestureHandlerRootView } from "react-native-gesture-handler";
import { SafeAreaProvider } from "react-native-safe-area-context";
import { PortalHost } from "@rn-primitives/portal";
import { QueryClientProvider } from "@tanstack/react-query";

import { ForceUpdateScreen } from "@/components/ForceUpdateScreen";
import { OfflineBanner } from "@/components/OfflineBanner";
import { useMinVersionCheck } from "@/lib/forceUpdate";
import { queryClient } from "@/lib/query-client";

export default function RootLayout() {
  // Force-update gate runs FIRST. While loading we render a transparent
  // placeholder (no flash of the real app); on update-required we replace
  // the entire stack with ForceUpdateScreen; otherwise we render the app.
  // Push / IAP / OTA init happen only when the gate is satisfied so users
  // on a force-blocked old build don't trigger them.
  const minVersion = useMinVersionCheck();

  useEffect(() => {
    if (minVersion.status !== "ok") return;

    async function checkForUpdates() {
      if (__DEV__) return;
      try {
        const update = await Updates.checkForUpdateAsync();
        if (update.isAvailable) {
          await Updates.fetchUpdateAsync();
          await Updates.reloadAsync();
        }
      } catch (e) {
        // Fail open — don't block app launch on update server issues
      }
    }
    checkForUpdates();
  }, [minVersion.status]);

  let content: React.ReactNode;
  if (minVersion.status === "loading") {
    // Transparent placeholder — same background as the rest of the app, no
    // visible flash. Avoids briefly rendering the real stack before the
    // gate resolves (one extra tick on a hot start).
    content = <View className="flex-1 bg-background" />;
  } else if (minVersion.status === "update-required") {
    content = (
      <>
        <StatusBar style="auto" />
        <ForceUpdateScreen message={minVersion.message} storeURL={minVersion.storeURL} />
      </>
    );
  } else {
    content = (
      <>
        <StatusBar style="auto" />
        <Stack screenOptions={{ headerShown: false }} />
      </>
    );
  }

  return (
    <QueryClientProvider client={queryClient}>
      <GestureHandlerRootView style={{ flex: 1 }}>
        <SafeAreaProvider>
          <OfflineBanner />
          <ErrorBoundary>
            {content}
          </ErrorBoundary>
          <PortalHost />
        </SafeAreaProvider>
      </GestureHandlerRootView>
    </QueryClientProvider>
  );
}

type ErrorBoundaryState = {
  hasError: boolean;
  error: Error | null;
};

class ErrorBoundary extends Component<
  { children: React.ReactNode },
  ErrorBoundaryState
> {
  constructor(props: { children: React.ReactNode }) {
    super(props);
    this.state = { hasError: false, error: null };
  }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, errorInfo: React.ErrorInfo) {
    console.error("ErrorBoundary caught an error:", error, errorInfo);
  }

  handleRetry = () => {
    this.setState({ hasError: false, error: null });
  };

  render() {
    if (this.state.hasError) {
      return (
        <View className="flex-1 items-center justify-center bg-background p-6">
          <Text className="text-foreground text-lg font-semibold mb-2">
            Something went wrong
          </Text>
          <Text className="text-muted-foreground text-sm text-center mb-6">
            {this.state.error?.message ?? "An unexpected error occurred."}
          </Text>
          <Button title="Retry" onPress={this.handleRetry} />
        </View>
      );
    }

    return this.props.children;
  }
}
