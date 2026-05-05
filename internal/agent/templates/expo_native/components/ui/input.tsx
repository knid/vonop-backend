import * as React from "react";
import { TextInput, type TextInputProps } from "react-native";
import { cn } from "@/lib/utils";

export interface InputProps extends TextInputProps {
  className?: string;
}

const Input = React.forwardRef<React.ElementRef<typeof TextInput>, InputProps>(
  ({ className, editable = true, ...props }, ref) => {
    return (
      // NativeWind v4 does not support focus: pseudo-classes.
      // Callers needing a visible focus ring should manage focused state via
      // onFocus/onBlur and toggle a custom className. See app/index.tsx for an example.
      <TextInput
        ref={ref}
        editable={editable}
        className={cn(
          "h-10 w-full rounded-md border border-border bg-background px-3 py-2 text-base text-foreground",
          "placeholder:text-muted-foreground",
          !editable && "opacity-50",
          className
        )}
        {...props}
      />
    );
  }
);

Input.displayName = "Input";

export { Input };
