import "react-native-gesture-handler";

import "../global.css";

import { Stack } from "expo-router";
import { StatusBar } from "expo-status-bar";
import { useEffect } from "react";
import * as Updates from "expo-updates";
import { GestureHandlerRootView } from "react-native-gesture-handler";
import { SafeAreaProvider } from "react-native-safe-area-context";
import { PortalHost } from "@rn-primitives/portal";

import { View } from "@/components/tw";
import { ForceUpdateScreen } from "@/components/ForceUpdateScreen";
import { useMinVersionCheck } from "@/lib/forceUpdate";
import { initIAP } from "@/lib/iap";
import { init as initLocalNotifications } from "@/lib/localNotifications";
import { initPush } from "@/lib/push";

export default function RootLayout() {
  // Force-update gate runs FIRST. While loading we render a transparent
  // placeholder (no flash of the real app); on update-required we replace
  // the entire stack with ForceUpdateScreen; otherwise we render the app.
  // Push / IAP / OTA init happen only when the gate is satisfied so users
  // on a force-blocked old build don't trigger them.
  const minVersion = useMinVersionCheck();

  useEffect(() => {
    if (minVersion.status !== "ok") return;

    initPush();
    initIAP();
    initLocalNotifications();

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
    <GestureHandlerRootView style={{ flex: 1 }}>
      <SafeAreaProvider>
        {content}
        <PortalHost name="root" />
      </SafeAreaProvider>
    </GestureHandlerRootView>
  );
}
