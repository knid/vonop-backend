import {
  useCssElement,
  useNativeVariable as useFunctionalVariable,
} from "react-native-css";

import { Link as RouterLink } from "expo-router";
import React from "react";
import {
  View as RNView,
  Text as RNText,
  ScrollView as RNScrollView,
  Pressable as RNPressable,
  TextInput as RNTextInput,
  TouchableHighlight as RNTouchableHighlight,
  StyleSheet,
} from "react-native";

const IS_WEB = process.env.EXPO_OS === "web";

// ---------------------------------------------------------------------------
// CSS variable hook
// ---------------------------------------------------------------------------

export const useCSSVariable =
  process.env.EXPO_OS !== "web"
    ? useFunctionalVariable
    : (variable: string) => `var(${variable})`;

// ---------------------------------------------------------------------------
// Link (expo-router)
// ---------------------------------------------------------------------------

function CSSLink(
  props: React.ComponentProps<typeof RouterLink> & { className?: string }
) {
  return useCssElement(RouterLink, props, { className: "style" });
}

export const Link = IS_WEB
  ? RouterLink
  : Object.assign(CSSLink, {
      Trigger: RouterLink.Trigger,
      Menu: RouterLink.Menu,
      MenuAction: RouterLink.MenuAction,
      Preview: RouterLink.Preview,
    });

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

export type ViewProps = React.ComponentProps<typeof RNView> & {
  className?: string;
};

export const View = IS_WEB
  ? RNView
  : (props: ViewProps) => useCssElement(RNView, props, { className: "style" });

// @ts-expect-error RNView has displayName but TS disagrees
View.displayName = IS_WEB ? "View" : "CSS(View)";

// ---------------------------------------------------------------------------
// Text
// ---------------------------------------------------------------------------

export const Text = IS_WEB
  ? RNText
  : (props: React.ComponentProps<typeof RNText> & { className?: string }) =>
      useCssElement(RNText, props, { className: "style" });

// @ts-expect-error RNText has displayName but TS disagrees
Text.displayName = IS_WEB ? "Text" : "CSS(Text)";

// ---------------------------------------------------------------------------
// ScrollView
// ---------------------------------------------------------------------------

export const ScrollView = IS_WEB
  ? RNScrollView
  : (
      props: React.ComponentProps<typeof RNScrollView> & {
        className?: string;
        contentContainerClassName?: string;
      }
    ) =>
      useCssElement(RNScrollView, props, {
        className: "style",
        contentContainerClassName: "contentContainerStyle",
      });

// @ts-expect-error RNScrollView has displayName but TS disagrees
ScrollView.displayName = IS_WEB ? "ScrollView" : "CSS(ScrollView)";

// ---------------------------------------------------------------------------
// Pressable
// ---------------------------------------------------------------------------

export const Pressable = IS_WEB
  ? RNPressable
  : (
      props: React.ComponentProps<typeof RNPressable> & {
        className?: string;
      }
    ) => useCssElement(RNPressable, props, { className: "style" });

// @ts-expect-error RNPressable has displayName but TS disagrees
Pressable.displayName = IS_WEB ? "Pressable" : "CSS(Pressable)";

// ---------------------------------------------------------------------------
// TextInput
// ---------------------------------------------------------------------------

export const TextInput = IS_WEB
  ? RNTextInput
  : (
      props: React.ComponentProps<typeof RNTextInput> & {
        className?: string;
      }
    ) => useCssElement(RNTextInput, props, { className: "style" });

// @ts-expect-error RNTextInput has displayName but TS disagrees
TextInput.displayName = IS_WEB ? "TextInput" : "CSS(TextInput)";

// ---------------------------------------------------------------------------
// TouchableHighlight
// ---------------------------------------------------------------------------

function XXTouchableHighlight(
  props: React.ComponentProps<typeof RNTouchableHighlight>
) {
  const { underlayColor, ...style } = StyleSheet.flatten(props.style) || {};
  return (
    <RNTouchableHighlight
      {...props}
      style={style}
      underlayColor={underlayColor as string}
    />
  );
}

function CSSTouchableHighlight(
  props: React.ComponentProps<typeof RNTouchableHighlight> & {
    className?: string;
  }
) {
  return useCssElement(XXTouchableHighlight, props, { className: "style" });
}

export const TouchableHighlight = IS_WEB
  ? RNTouchableHighlight
  : CSSTouchableHighlight;

// @ts-expect-error RNTouchableHighlight has displayName but TS disagrees
TouchableHighlight.displayName = IS_WEB
  ? "TouchableHighlight"
  : "CSS(TouchableHighlight)";
