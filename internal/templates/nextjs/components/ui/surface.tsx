import * as React from "react";
import { cn } from "@/lib/utils";

export interface SurfaceProps extends React.HTMLAttributes<HTMLDivElement> {
  variant?: "default" | "muted" | "elevated" | "outline";
}

const surfaceVariants = {
  default: "border-border/60 bg-card/85 shadow-[var(--shadow-soft)]",
  muted: "border-border/50 bg-muted/55",
  elevated: "border-border/50 bg-elevated/90 shadow-[var(--shadow-lifted)]",
  outline: "border-border/70 bg-background/70",
};

export function Surface({
  className,
  variant = "default",
  ...props
}: SurfaceProps) {
  return (
    <div
      className={cn(
        "rounded-2xl border p-5 text-card-foreground backdrop-blur-sm sm:p-6",
        surfaceVariants[variant],
        className
      )}
      {...props}
    />
  );
}

export function BentoGrid({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn("grid gap-4 sm:grid-cols-2 lg:grid-cols-12", className)}
      {...props}
    />
  );
}

export interface MetricProps extends React.HTMLAttributes<HTMLDivElement> {
  label: React.ReactNode;
  value: React.ReactNode;
  helper?: React.ReactNode;
}

export function Metric({
  className,
  label,
  value,
  helper,
  ...props
}: MetricProps) {
  return (
    <div className={cn("space-y-2", className)} {...props}>
      <p className="text-sm font-medium text-muted-foreground">{label}</p>
      <p className="text-3xl font-semibold tracking-tight text-foreground tabular-nums">
        {value}
      </p>
      {helper ? <p className="text-sm text-muted-foreground">{helper}</p> : null}
    </div>
  );
}
