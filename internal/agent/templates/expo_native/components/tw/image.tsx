import { useCssElement } from "react-native-css";
import React from "react";
import { StyleSheet } from "react-native";
import Animated from "react-native-reanimated";
import { Image as RNImage } from "expo-image";

const IS_WEB = process.env.EXPO_OS === "web";

const AnimatedExpoImage = Animated.createAnimatedComponent(RNImage);

export type ImageProps = React.ComponentProps<typeof RNImage> & {
  className?: string;
};

function CSSImage(props: React.ComponentProps<typeof RNImage>) {
  // @ts-expect-error: Remap objectFit style to contentFit property
  const { objectFit, objectPosition, ...style } =
    StyleSheet.flatten(props.style) || {};

  return (
    <RNImage
      {...props}
      style={style}
      contentFit={objectFit}
      contentPosition={objectPosition}
    />
  );
}

function CSSImageWrapper(
  props: React.ComponentProps<typeof RNImage> & { className?: string }
) {
  return useCssElement(CSSImage, props, { className: "style" });
}

export const Image = IS_WEB ? RNImage : CSSImageWrapper;

// @ts-expect-error RNImage has displayName but TS disagrees
Image.displayName = IS_WEB ? "Image" : "CSS(Image)";

// ---------------------------------------------------------------------------
// Animated Image
// ---------------------------------------------------------------------------

function CSSAnimatedImage(
  props: React.ComponentProps<typeof AnimatedExpoImage>
) {
  // @ts-expect-error: Remap objectFit style to contentFit property
  const { objectFit, objectPosition, ...style } =
    StyleSheet.flatten(props.style) || {};

  return (
    <AnimatedExpoImage
      {...props}
      style={style}
      contentFit={objectFit}
      contentPosition={objectPosition}
    />
  );
}

function CSSAnimatedImageWrapper(
  props: React.ComponentProps<typeof AnimatedExpoImage> & {
    className?: string;
  }
) {
  return useCssElement(CSSAnimatedImage, props, { className: "style" });
}

export const AnimatedImage = IS_WEB
  ? AnimatedExpoImage
  : CSSAnimatedImageWrapper;

// @ts-expect-error AnimatedExpoImage has displayName but TS disagrees
AnimatedImage.displayName = IS_WEB ? "AnimatedImage" : "CSS(AnimatedImage)";
