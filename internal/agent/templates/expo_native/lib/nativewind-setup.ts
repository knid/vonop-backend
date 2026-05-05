// nativewind-setup.ts — runs once at app start (imported from app/_layout.tsx
// before any screen is rendered) to register `className` → `style` mapping
// for components NativeWind doesn't auto-handle.
//
// Without this, `<Animated.View className="px-5">` silently drops the
// className: react-native-reanimated's Animated.* components don't forward
// `className` through to a NativeWind-styled element by default, so every
// screen wrapping content in `<Animated.View>` renders edge-to-edge with
// no padding. Same story for Animated.ScrollView / Animated.Text. Symptom
// in the studio: headers and labels touch the iframe's left/right edges,
// chip rails overflow the screen.
//
// Add new entries here whenever a third-party component needs className
// support — the cssInterop call is a one-shot registration, repeating it
// is harmless but only the first one takes effect.

import { cssInterop } from "nativewind";
import Animated from "react-native-reanimated";

cssInterop(Animated.View, { className: "style" });
cssInterop(Animated.ScrollView, { className: "style" });
cssInterop(Animated.Text, { className: "style" });
cssInterop(Animated.Image, { className: "style" });
cssInterop(Animated.FlatList, { className: "style" });
