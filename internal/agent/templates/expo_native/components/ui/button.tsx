import * as React from "react";
import { type PressableProps } from "react-native";
import { cva, type VariantProps } from "class-variance-authority";
import { Pressable } from "@/components/tw";
import { cn } from "@/lib/utils";

const buttonVariants = cva(
  "flex flex-row items-center justify-center rounded-md text-sm font-medium",
  {
    variants: {
      variant: {
        default:
          "bg-slate-900 text-slate-50 active:opacity-90",
        destructive:
          "bg-red-500 text-slate-50 active:opacity-90",
        outline:
          "border border-slate-200 bg-transparent active:bg-slate-100",
        secondary:
          "bg-slate-100 text-slate-900 active:bg-slate-200",
        ghost:
          "bg-transparent active:bg-slate-100",
        link:
          "text-slate-900 underline-offset-4 underline",
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
