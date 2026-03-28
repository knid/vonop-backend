"use client";

import React from "react";
import { formatPrice, type StorefrontProduct } from "@/lib/wemob-store";

interface PriceDisplayProps {
  product?: StorefrontProduct;
  size?: "sm" | "md" | "lg";
  className?: string;
}

export function PriceDisplay({ product, size = "md", className = "" }: PriceDisplayProps) {
  const sizeClasses = {
    sm: "text-sm",
    md: "text-lg",
    lg: "text-3xl tracking-tight",
  };

  if (!product) return null;

  const onSale = product.compare_at_price > 0 && product.compare_at_price > product.price_in_cents;
  const pct = onSale ? Math.round((1 - product.price_in_cents / product.compare_at_price) * 100) : 0;

  return (
    <div className={"flex flex-wrap items-center gap-2 " + className}>
      <span className={sizeClasses[size] + " font-semibold " + (onSale ? "text-rose-600 dark:text-rose-400" : "text-foreground")}>
        {formatPrice(product.price_in_cents)}
      </span>
      {onSale && (
        <>
          <span className={"text-muted-foreground/50 line-through decoration-muted-foreground/30 " + (size === "lg" ? "text-base" : "text-xs")}>
            {formatPrice(product.compare_at_price)}
          </span>
          <span className="rounded-full bg-rose-100 px-2 py-0.5 text-[11px] font-semibold text-rose-600 dark:bg-rose-500/10 dark:text-rose-400">
            {"-" + pct + "%"}
          </span>
        </>
      )}
      {product.in_stock === false && (
        <span className="rounded-full bg-muted/80 px-2.5 py-0.5 text-[11px] font-medium uppercase tracking-wider text-muted-foreground">
          {{t.outOfStock}}
        </span>
      )}
    </div>
  );
}
