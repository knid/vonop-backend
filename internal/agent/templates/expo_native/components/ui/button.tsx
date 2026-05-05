import * as React from "react";
import { Pressable, type PressableProps } from "react-native";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const buttonVariants = cva(
  "flex flex-row items-center justify-center rounded-md text-sm font-medium",
  {
    variants: {
      variant: {
        default:
          "bg-primary text-primary-foreground active:opacity-90",
        destructive:
          "bg-destructive text-destructive-foreground active:opacity-90",
        outline:
          "border border-border bg-transparent active:bg-muted",
        secondary:
          "bg-secondary text-secondary-foreground active:bg-muted",
        ghost:
          "bg-transparent active:bg-muted",
        link:
          "text-primary underline-offset-4 underline",
      },
      size: {
        default: "h-10 px-4 py-2",
        sm: "h-9 rounded-md px-3",
        lg: "h-11 rounded-md px-8",
        icon: "h-10 w-10",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  }
);

export interface ButtonProps
  extends PressableProps,
    VariantProps<typeof buttonVariants> {
  className?: string;
}

const Button = React.forwardRef<
  React.ElementRef<typeof Pressable>,
  ButtonProps
>(({ className, variant, size, ...props }, ref) => {
  return (
    <Pressable
      ref={ref}
      className={cn(buttonVariants({ variant, size }), className)}
      {...props}
    />
  );
});

Button.displayName = "Button";

export { Button, buttonVariants };
