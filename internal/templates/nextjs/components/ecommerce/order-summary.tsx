"use client";

import React from "react";
import { formatPrice, type StorefrontCart } from "@/lib/wemob-store";

interface OrderSummaryProps {
  cart: StorefrontCart;
  className?: string;
}

export function OrderSummary({ cart, className = "" }: OrderSummaryProps) {
  return (
    <div className={"rounded-2xl bg-muted/30 p-6 shadow-sm " + className}>
      <h3 className="mb-5 text-sm font-medium uppercase tracking-[0.15em] text-muted-foreground">
        {{t.orderSummary}}
      </h3>
      <div className="space-y-3">
        <div className="flex items-center justify-between text-sm">
          <span className="text-muted-foreground">
            Subtotal ({cart.items.reduce((sum, i) => sum + i.quantity, 0)} items)
          </span>
          <span>{formatPrice(cart.subtotal_cents)}</span>
        </div>
        {cart.discount_cents > 0 && (
          <div className="flex items-center justify-between text-sm">
            <span className="text-green-600">
              Discount{cart.discount_code ? " (" + cart.discount_code + ")" : ""}
            </span>
            <span className="text-green-600">-{formatPrice(cart.discount_cents)}</span>
          </div>
        )}
        <div className="flex items-center justify-between text-sm">
          <span className="text-muted-foreground">{{t.shipping}}</span>
          <span className="text-muted-foreground">{{t.calculatedAtCheckout}}</span>
        </div>
        <div className="border-t border-border/40 pt-4">
          <div className="flex items-center justify-between">
            <span className="font-semibold">Total</span>
            <span className="text-2xl font-semibold tracking-tight">{formatPrice(cart.total_cents)}</span>
          </div>
        </div>
      </div>
    </div>
  );
}
