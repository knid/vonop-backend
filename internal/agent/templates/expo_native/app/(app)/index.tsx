import { View } from "@/components/tw";
import { Button } from "@/components/ui/button";
import { Text } from "@/components/ui/text";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";

export default function Index() {
  return (
    <View className="flex-1 items-center justify-center bg-background p-4 gap-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>Wemob template ready</CardTitle>
        </CardHeader>
        <CardContent>
          <Text className="mb-4">react-native-reusables installed with variants.</Text>
          <View className="flex-row gap-2">
            <Button onPress={() => console.log("Default")}>
              <Text>Default</Text>
            </Button>
            <Button variant="destructive" onPress={() => console.log("Danger")}>
              <Text>Destructive</Text>
            </Button>
            <Button variant="outline" onPress={() => console.log("Outline")}>
              <Text>Outline</Text>
            </Button>
          </View>
        </CardContent>
      </Card>
    </View>
  );
}
