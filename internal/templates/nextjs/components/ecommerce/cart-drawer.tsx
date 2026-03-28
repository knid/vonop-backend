"use client";

import React from "react";
import Link from "next/link";
import { X, ShoppingBag, Check } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { useCart, formatPrice } from "@/lib/wemob-store";
import { CartItemRow } from "@/components/ecommerce/cart-item-row";
import { EmptyState } from "@/components/ecommerce/empty-state";

// Free shipping threshold in cents — change to match your store
const FREE_SHIPPING_THRESHOLD = 5000;

interface CartDrawerProps {
  isOpen: boolean;
  onClose: () => void;
}

export function CartDrawer({ isOpen, onClose }: CartDrawerProps) {
  const { cart, isLoading, itemCount } = useCart();

  React.useEffect(() => {
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    if (isOpen) {
      document.addEventListener("keydown", handleEscape);
      document.body.style.overflow = "hidden";
    }
    return () => {
      document.removeEventListener("keydown", handleEscape);
      document.body.style.overflow = "";
    };
  }, [isOpen, onClose]);

  const shippingProgress = cart ? Math.min(cart.subtotal_cents / FREE_SHIPPING_THRESHOLD, 1) : 0;
  const shippingRemaining = cart ? FREE_SHIPPING_THRESHOLD - cart.subtotal_cents : FREE_SHIPPING_THRESHOLD;

  return (
    <AnimatePresence>
      {isOpen && (
        <>
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            className="fixed inset-0 z-50 bg-black/40 backdrop-blur-sm"
            onClick={onClose}
          />
          <motion.div
            initial={{ x: "100%" }}
            animate={{ x: 0 }}
            exit={{ x: "100%" }}
            transition={{ type: "spring", damping: 25, stiffness: 200 }}
            className="fixed right-0 top-0 z-50 flex h-full w-full max-w-md flex-col bg-background shadow-2xl"
          >
            <div className="flex items-center justify-between border-b border-border/40 px-5 py-4">
              <h2 className="text-sm font-medium uppercase tracking-[0.15em] text-foreground">
                {{t.cart}} ({itemCount})
              </h2>
              <button onClick={onClose} className="rounded-full p-1.5 hover:bg-muted" aria-label="{{t.closeCart}}">
                <X className="h-4 w-4" />
              </button>
            </div>

            <div className="flex-1 overflow-y-auto px-5 py-5">
              {isLoading ? (
                <div className="space-y-4">
                  {[1, 2, 3].map((i) => (
                    <div key={i} className="h-20 animate-pulse rounded-xl bg-muted" />
                  ))}
                </div>
              ) : !cart || cart.items.length === 0 ? (
                <EmptyState
                  icon={<ShoppingBag className="h-10 w-10" />}
                  title="{{t.emptyCartTitle}}"
                  description="{{t.emptyCartDesc}}"
                />
              ) : (
                <div className="space-y-3">
                  {cart.items.map((item) => (
                    <CartItemRow key={item.id} item={item} compact />
                  ))}
                </div>
              )}
            </div>

            {cart && cart.items.length > 0 && (
              <div className="border-t border-border/40 px-5 py-5">
                {/* Free shipping progress */}
                <div className="mb-4">
                  {shippingRemaining > 0 ? (
                    <>
                      <p className="mb-1.5 text-xs text-muted-foreground">
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

                <div className="mb-3 flex items-center justify-between text-sm">
                  <span className="text-muted-foreground">Subtotal</span>
                  <span className="font-semibold">{formatPrice(cart.subtotal_cents)}</span>
                </div>
                {cart.discount_cents > 0 && (
                  <div className="mb-3 flex items-center justify-between text-sm">
                    <span className="text-green-600">Discount</span>
                    <span className="font-semibold text-green-600">-{formatPrice(cart.discount_cents)}</span>
                  </div>
                )}
                <div className="mb-5 flex items-center justify-between">
                  <span className="font-semibold">Total</span>
                  <span className="text-xl font-semibold tracking-tight">{formatPrice(cart.total_cents)}</span>
                </div>
                <p className="mb-3 text-center text-[11px] text-muted-foreground">
                  {{t.taxesShippingAtCheckout}}
                </p>
                <div className="flex flex-col gap-3">
                  <Link
                    href="/checkout"
                    onClick={onClose}
                    className="flex w-full items-center justify-center gap-2 rounded-full bg-foreground px-4 py-3 text-sm font-semibold text-background transition-opacity hover:opacity-90"
                  >
                    {{t.secureCheckout}}
                  </Link>
                  <Link
                    href="/cart"
                    onClick={onClose}
                    className="flex w-full items-center justify-center text-sm font-medium text-muted-foreground underline underline-offset-4 transition-colors hover:text-foreground"
                  >
                    {{t.viewCart}}
                  </Link>
                </div>
              </div>
            )}
          </motion.div>
        </>
      )}
    </AnimatePresence>
  );
}
