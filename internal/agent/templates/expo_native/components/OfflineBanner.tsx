import { View, Text } from "react-native";
import { useNetworkStatus } from "@/lib/network";

export function OfflineBanner() {
  const { isConnected } = useNetworkStatus();

  if (isConnected) return null;

  return (
    <View className="absolute top-0 left-0 right-0 z-50 bg-red-500 px-4 py-2 items-center">
      <Text className="text-white text-sm font-semibold">No internet connection</Text>
    </View>
  );
}
