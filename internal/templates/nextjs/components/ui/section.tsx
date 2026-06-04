import * as React from "react";
import { cn } from "@/lib/utils";

export function PageShell({
  className,
  ...props
}: React.HTMLAttributes<HTMLElement>) {
  return (
    <main
      className={cn("min-h-screen bg-background text-foreground", className)}
      {...props}
    />
  );
}

export interface SectionProps extends React.HTMLAttributes<HTMLElement> {
  variant?: "default" | "muted" | "tint" | "inverse";
  size?: "sm" | "md" | "lg";
}

const sectionVariants = {
  default: "bg-background",
  muted: "bg-muted/45",
  tint: "bg-primary/5",
  inverse: "bg-foreground text-background",
};

const sectionSizes = {
  sm: "py-12 sm:py-16",
  md: "py-16 sm:py-20",
  lg: "py-20 sm:py-28",
};

export function Section({
  className,
  variant = "default",
  size = "lg",
  ...props
}: SectionProps) {
  return (
    <section
      className={cn(sectionVariants[variant], sectionSizes[size], className)}
      {...props}
    />
  );
}

export interface SectionHeaderProps extends React.HTMLAttributes<HTMLDivElement> {
  eyebrow?: React.ReactNode;
  title?: React.ReactNode;
  description?: React.ReactNode;
  align?: "left" | "center";
}

export function SectionHeader({
  className,
  eyebrow,
  title,
  description,
  align = "left",
  ...props
}: SectionHeaderProps) {
  return (
    <div
      className={cn(
        "max-w-3xl",
        align === "center" && "mx-auto text-center",
        className
      )}
      {...props}
    >
      {eyebrow ? (
        <p className="mb-3 text-xs font-semibold uppercase tracking-[0.18em] text-primary">
          {eyebrow}
        </p>
      ) : null}
      {title ? (
        <h2 className="text-3xl font-semibold leading-tight text-foreground text-balance sm:text-5xl">
          {title}
        </h2>
      ) : null}
      {description ? (
        <p className="mt-4 text-base leading-7 text-muted-foreground sm:text-lg">
          {description}
        </p>
      ) : null}
    </div>
  );
}
