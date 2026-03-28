"use client";

import React, { useState } from "react";
import Link from "next/link";
import { ShoppingBag, ArrowLeft, Check, Lock, Truck } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { useCart, useProducts, formatPrice } from "@/lib/wemob-store";
import { CartItemRow } from "@/components/ecommerce/cart-item-row";
import { OrderSummary } from "@/components/ecommerce/order-summary";
import { ProductCard } from "@/components/ecommerce/product-card";
import { EmptyState } from "@/components/ecommerce/empty-state";

// Free shipping threshold in cents — change to match your store
const FREE_SHIPPING_THRESHOLD = 5000;

export default function CartPage() {
  const { cart, isLoading, itemCount, applyDiscount, removeDiscountCode } = useCart();
  const { products: allProducts } = useProducts();

  const [discountCode, setDiscountCode] = useState("");
  const [discountLoading, setDiscountLoading] = useState(false);
  const [discountError, setDiscountError] = useState("");
  const [discountSuccess, setDiscountSuccess] = useState(false);

  const cartProductIds = cart?.items.map((i) => i.product_id) ?? [];
  const suggestions = allProducts?.filter((p) => !cartProductIds.includes(p.id) && p.in_stock !== false).slice(0, 4) ?? [];

  const handleApplyDiscount = async () => {
    if (!discountCode.trim()) return;
    setDiscountLoading(true);
    setDiscountError("");
    setDiscountSuccess(false);
    try {
      await applyDiscount(discountCode.trim());
      setDiscountSuccess(true);
      setTimeout(() => setDiscountSuccess(false), 2000);
    } catch {
      setDiscountError("{{t.invalidDiscountCode}}");
    } finally {
      setDiscountLoading(false);
    }
  };

  const shippingProgress = cart ? Math.min(cart.subtotal_cents / FREE_SHIPPING_THRESHOLD, 1) : 0;
  const shippingRemaining = cart ? FREE_SHIPPING_THRESHOLD - cart.subtotal_cents : FREE_SHIPPING_THRESHOLD;

  if (isLoading) {
    return (
      <div className="mx-auto max-w-7xl px-4 py-12 sm:px-6 sm:py-16 lg:px-8">
        <h1 className="text-3xl font-semibold tracking-tight text-foreground sm:text-4xl">{{t.shoppingCart}}</h1>
        <div className="mt-8 space-y-4">
          {[1, 2, 3].map((i) => (
            <div key={i} className="h-24 animate-pulse rounded-xl bg-muted" />
          ))}
        </div>
      </div>
    );
  }

  if (!cart || itemCount === 0) {
    return (
      <div className="mx-auto max-w-7xl px-4 py-12 sm:px-6 sm:py-16 lg:px-8">
        <h1 className="text-3xl font-semibold tracking-tight text-foreground sm:text-4xl">{{t.shoppingCart}}</h1>
        <div className="mt-12">
          <EmptyState
            icon={<ShoppingBag className="h-10 w-10" />}
            title="{{t.emptyCartTitle}}"
            description="{{t.emptyCartPageDesc}}"
            action={<Link href="/products" className="inline-flex h-11 items-center rounded-full bg-foreground px-6 text-sm font-medium text-background transition-opacity hover:opacity-90">{{t.continueShopping}}</Link>}
          />
        </div>
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-7xl px-4 py-12 sm:px-6 sm:py-16 lg:px-8">
      <div className="flex items-center justify-between">
        <h1 className="text-3xl font-semibold tracking-tight text-foreground sm:text-4xl">
          {{t.shoppingCart}} ({itemCount})
        </h1>
        <Link
          href="/products"
          className="inline-flex items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
        >
          <ArrowLeft className="h-4 w-4" />
          {{t.continueShopping}}
        </Link>
      </div>

      {/* Free shipping progress */}
      <div className="mt-6">
        {shippingRemaining > 0 ? (
          <>
            <p className="mb-2 text-xs text-muted-foreground">
              {{{te.awayFromFreeShipping}}}
            </p>
            <div className="h-1.5 overflow-hidden rounded-full bg-muted">
              <motion.div
                className="h-full rounded-full bg-foreground"
                initial={{ width: 0 }}
                animate={{ width: (shippingProgress * 100) + "%" }}
                transition={{ duration: 0.5 }}
              />
            </div>
          </>
        ) : (
          <div className="flex items-center gap-1.5 text-xs text-green-600 dark:text-green-400">
            <Check className="h-3.5 w-3.5" />
            <span>{{t.qualifyFreeShipping}}</span>
          </div>
        )}
      </div>

      <div className="mt-8 gap-8 lg:grid lg:grid-cols-[1fr_400px]">
        <div>
          <AnimatePresence>
            <div className="space-y-3">
              {cart.items.map((item) => (
                <CartItemRow key={item.id} item={item} />
              ))}
            </div>
          </AnimatePresence>

          <div className="mt-6 flex items-end gap-3">
            <div className="flex-1">
              <label htmlFor="discount" className="mb-1.5 block text-[11px] font-medium uppercase tracking-[0.15em] text-muted-foreground/60">
                {{t.discountCode}}
              </label>
              <input
                id="discount"
                type="text"
                value={discountCode}
                onChange={(e) => {
                  setDiscountCode(e.target.value);
                  setDiscountError("");
                }}
                placeholder="{{t.enterCode}}"
                className="h-11 w-full rounded-full border border-border/60 bg-background px-5 text-sm text-foreground placeholder:text-muted-foreground focus:border-foreground focus:outline-none"
              />
              {discountError && (
                <p className="mt-1 text-sm text-red-600 dark:text-red-400">{discountError}</p>
              )}
            </div>
            <button
              onClick={handleApplyDiscount}
              disabled={discountLoading || !discountCode.trim() || discountSuccess}
              className={"h-11 rounded-full px-5 text-sm font-medium transition-colors disabled:opacity-50 " + (discountSuccess ? "bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400" : "bg-muted text-foreground hover:bg-muted/80")}
            >
              {discountSuccess ? (
                <span className="flex items-center gap-1"><Check className="h-3.5 w-3.5" /> {{t.applied}}</span>
              ) : discountLoading ? "{{t.applying}}" : "{{t.apply}}"}
            </button>
          </div>

          {cart.discount_code && (
            <div className="mt-3 flex items-center gap-2 text-sm">
              <span className="rounded-full bg-green-100 px-3 py-0.5 font-medium text-green-700 dark:bg-green-900/30 dark:text-green-400">
                {cart.discount_code}
              </span>
              <button
                onClick={() => removeDiscountCode()}
                className="text-muted-foreground transition-colors hover:text-foreground"
              >
                Remove
              </button>
            </div>
          )}
        </div>

        <div className="mt-8 lg:mt-0 lg:sticky lg:top-24 lg:self-start">
          <OrderSummary cart={cart} />
          <Link
            href="/checkout"
            className="mt-4 flex h-14 w-full items-center justify-center gap-2 rounded-full bg-foreground text-sm font-semibold text-background transition-opacity hover:opacity-90"
          >
            <Lock className="h-4 w-4" />
            {{t.secureCheckout}}
          </Link>
          <div className="mt-4 rounded-xl border border-border/40 p-4 text-center">
            <div className="flex items-center justify-center gap-4 text-xs text-muted-foreground">
              <div className="flex items-center gap-1">
                <Truck className="h-3.5 w-3.5" />
                <span>{{t.freeShippingOver50}}</span>
              </div>
            </div>
            <p className="mt-2 text-[11px] text-muted-foreground/50">
              {"Estimated delivery: " + new Date(Date.now() + 5 * 24 * 60 * 60 * 1000).toLocaleDateString("en-US", { month: "short", day: "numeric" }) + " \u2013 " + new Date(Date.now() + 7 * 24 * 60 * 60 * 1000).toLocaleDateString("en-US", { month: "short", day: "numeric" })}
            </p>
          </div>
        </div>
      </div>

      {/* {{t.youMightAlsoLike}} */}
      {suggestions.length > 0 && (
        <div className="mt-16 border-t border-border/40 pt-12">
          <h2 className="text-xl font-semibold tracking-tight text-foreground">{{t.youMightAlsoLike}}</h2>
          <div className="mt-6 grid grid-cols-2 gap-6 sm:gap-8 lg:grid-cols-4">
            {suggestions.map((p) => (
              <ProductCard key={p.id} product={p} />
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
