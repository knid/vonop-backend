import * as React from "react";
import { Text as RNText, type TextProps as RNTextProps } from "react-native";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const textVariants = cva("text-slate-950", {
  variants: {
    variant: {
      default: "text-base",
      lead: "text-xl text-slate-700",
      large: "text-lg font-semibold",
      small: "text-sm font-medium leading-none",
      muted: "text-sm text-slate-500",
      blockquote: "text-base italic text-slate-700",
    },
  },
  defaultVariants: {
    variant: "default",
  },
});

export interface TextProps extends RNTextProps, VariantProps<typeof textVariants> {
  className?: string;
}

const Text = React.forwardRef<React.ElementRef<typeof RNText>, TextProps>(
  ({ className, variant, ...props }, ref) => {
    return (
      <RNText
        ref={ref}
        className={cn(textVariants({ variant }), className)}
        {...props}
      />
    );
  }
);

Text.displayName = "Text";

export { Text, textVariants };
