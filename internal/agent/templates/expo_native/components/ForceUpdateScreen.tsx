import { Linking, View } from "react-native";

import { Button } from "@/components/ui/button";
import { Text } from "@/components/ui/text";

/**
 * Full-screen, non-skippable update prompt rendered by app/_layout.tsx
 * when useMinVersionCheck() reports `update-required`.
 *
 * No back button, no skip — the floor exists because the operator decided
 * the older version must not continue running (security incident, broken
 * native module, billing key rotation). The user's only option is to
 * update via the configured store URL.
 *
 * The agent MAY customize the copy and styling but MUST keep the core
 * behavior: full-screen, non-dismissible, opens the store via Linking.
 */
type Props = {
  /** Operator-customizable message; falls back to a default if empty. */
  message?: string;
  /** App Store / Play Store URL the button opens. Empty → no button. */
  storeURL?: string;
};

const DEFAULT_MESSAGE = "A new version is required. Please update to continue.";

export function ForceUpdateScreen({ message, storeURL }: Props) {
  const copy = (message ?? "").trim() || DEFAULT_MESSAGE;
  const hasStore = !!(storeURL && storeURL.trim());

  const handlePress = async () => {
    if (!hasStore) return;
    try {
      await Linking.openURL(storeURL!);
    } catch {
      // Linking failure is rare on store-deployed builds; nothing to do
      // here — the user can still update from their store app manually.
    }
  };

  return (
    <View className="flex-1 items-center justify-center bg-background p-6 gap-6">
      <View className="w-full max-w-sm gap-3">
        <Text className="text-2xl font-semibold text-center">Update Required</Text>
        <Text className="text-base text-muted-foreground text-center">{copy}</Text>
      </View>
      {hasStore ? (
        <Button onPress={handlePress} accessibilityLabel="Update app">
          <Text>Update Now</Text>
        </Button>
      ) : null}
    </View>
  );
}
