import { useCssElement } from "react-native-css";
import React from "react";
import { StyleSheet } from "react-native";
import Animated from "react-native-reanimated";
import { Image as RNImage } from "expo-image";

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

export const Image = (
  props: React.ComponentProps<typeof RNImage> & { className?: string }
) => {
  return useCssElement(CSSImage, props, { className: "style" });
};

Image.displayName = "CSS(Image)";

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

export const AnimatedImage = (
  props: React.ComponentProps<typeof AnimatedExpoImage> & {
    className?: string;
  }
) => {
  return useCssElement(CSSAnimatedImage, props, { className: "style" });
};

AnimatedImage.displayName = "CSS(AnimatedImage)";
