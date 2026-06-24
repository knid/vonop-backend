import "react-native-gesture-handler";

import "../global.css";

// Side-effect import: registers cssInterop for Animated.* components so
// `<Animated.View className="px-5">` actually applies styles. MUST run
// before any Animated component is rendered, hence the top-level import.
import "@/lib/nativewind-setup";

import { Stack } from "expo-router";
import { StatusBar } from "expo-status-bar";
import { Component } from "react";
import { Button, Text, View } from "react-native";
import { GestureHandlerRootView } from "react-native-gesture-handler";
import { SafeAreaProvider } from "react-native-safe-area-context";
import { PortalHost } from "@rn-primitives/portal";
import { QueryClientProvider } from "@tanstack/react-query";

import { OfflineBanner } from "@/components/OfflineBanner";
import { queryClient } from "@/lib/query-client";

export default function RootLayout() {
  return (
    <QueryClientProvider client={queryClient}>
      <GestureHandlerRootView style={{ flex: 1 }}>
        <SafeAreaProvider>
          <OfflineBanner />
          <ErrorBoundary>
            <StatusBar style="auto" />
            <Stack screenOptions={{ headerShown: false }} />
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
