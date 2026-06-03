import * as React from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-full text-sm font-semibold tracking-normal transition-[background-color,border-color,color,box-shadow,transform,opacity] duration-200 ease-out focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/35 focus-visible:ring-offset-2 focus-visible:ring-offset-background active:scale-[0.98] disabled:pointer-events-none disabled:opacity-45 disabled:shadow-none disabled:active:scale-100 [&_svg]:pointer-events-none [&_svg]:size-4 [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground shadow-[var(--shadow-lifted)] shadow-primary/10 hover:bg-primary/90 hover:shadow-[var(--shadow-soft)]",
        destructive: "bg-destructive text-destructive-foreground shadow-sm shadow-destructive/20 hover:bg-destructive/90",
        outline: "border border-border/70 bg-background/80 text-foreground shadow-sm backdrop-blur hover:border-foreground/20 hover:bg-muted/70",
        secondary: "bg-secondary text-secondary-foreground shadow-sm shadow-secondary/10 hover:bg-secondary/90",
        ghost: "text-foreground hover:bg-muted/70 hover:text-foreground",
        link: "rounded-none px-0 text-primary underline-offset-4 hover:underline active:scale-100",
      },
      size: {
        default: "h-11 px-5 py-2.5",
        sm: "h-9 px-4 text-xs",
        lg: "h-12 px-7 text-base",
        xl: "h-14 px-9 text-base",
        icon: "size-10",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  }
);

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean;
}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : "button";
    return (
      <Comp
        className={cn(buttonVariants({ variant, size, className }))}
        ref={ref}
        {...props}
      />
    );
  }
);
Button.displayName = "Button";

export { Button, buttonVariants };
