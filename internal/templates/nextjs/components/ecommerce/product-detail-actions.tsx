"use client";

import React, { useState } from "react";
import { Minus, Plus, ShoppingCart, Check, Copy } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { useCart, type StorefrontProduct } from "@/lib/wemob-store";

interface ProductDetailActionsProps {
  product: StorefrontProduct;
}

export function ProductDetailActions({ product }: ProductDetailActionsProps) {
  const { addItem } = useCart();
  const [quantity, setQuantity] = useState(1);
  const [isAdding, setIsAdding] = useState(false);
  const [addedToCart, setAddedToCart] = useState(false);
  const [activeTab, setActiveTab] = useState<"description" | "shipping">("description");
  const [linkCopied, setLinkCopied] = useState(false);

  const outOfStock = product.in_stock === false;

  const handleAddToCart = async () => {
    if (outOfStock) return;
    setIsAdding(true);
    try {
      await addItem(product.id, quantity);
      setAddedToCart(true);
      setTimeout(() => setAddedToCart(false), 2000);
    } catch {
      // handled by cart provider
    } finally {
      setIsAdding(false);
    }
  };

  return (
    <>
      {/* Quantity & Add to Cart */}
      <div className="mt-8 space-y-4">
        {!outOfStock && (
          <div className="flex items-center gap-3">
            <span className="text-sm font-medium text-foreground">{{t.quantity}}</span>
            <div className="flex items-center rounded-xl border border-border/60">
              <button
                onClick={() => setQuantity(Math.max(1, quantity - 1))}
                className="flex h-10 w-10 items-center justify-center text-muted-foreground transition-colors hover:text-foreground active:scale-95"
                aria-label="{{t.decreaseQuantity}}"
              >
                <Minus className="h-4 w-4" />
              </button>
              <span className="flex h-10 w-12 items-center justify-center text-sm font-medium tabular-nums text-foreground">
                {quantity}
              </span>
              <button
                onClick={() => setQuantity(quantity + 1)}
                className="flex h-10 w-10 items-center justify-center text-muted-foreground transition-colors hover:text-foreground active:scale-95"
                aria-label="{{t.increaseQuantity}}"
              >
                <Plus className="h-4 w-4" />
              </button>
            </div>
          </div>
        )}

        <button
          onClick={handleAddToCart}
          disabled={outOfStock || isAdding}
          className="flex h-14 w-full items-center justify-center gap-2 rounded-full bg-foreground text-sm font-semibold text-background transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
        >
          <AnimatePresence mode="wait">
            {outOfStock ? (
              <span key="oos">Out of Stock</span>
            ) : addedToCart ? (
              <motion.span key="added" initial={{ scale: 0 }} animate={{ scale: 1 }} className="flex items-center gap-2">
                <Check className="h-4 w-4" /> {{t.addedToCart}}
              </motion.span>
            ) : (
              <motion.span key="add" initial={{ scale: 0.9, opacity: 0 }} animate={{ scale: 1, opacity: 1 }} className="flex items-center gap-2">
                <ShoppingCart className="h-4 w-4" />
                {isAdding ? "{{t.adding}}" : "{{t.addToCart}}"}
              </motion.span>
            )}
          </AnimatePresence>
        </button>
      </div>

      {/* Info Tabs */}
      <div className="mt-10 border-t border-border/40 pt-6">
        <div className="flex gap-6">
          {[{ id: "description" as const, label: "{{t.description}}" }, { id: "shipping" as const, label: "{{t.shippingAndReturns}}" }].map((tab) => (
            <button
              key={tab.id}
              onClick={() => setActiveTab(tab.id)}
              className={"relative pb-2 text-sm font-medium transition-colors " +
                (activeTab === tab.id ? "text-foreground" : "text-muted-foreground hover:text-foreground")}
            >
              {tab.label}
              {activeTab === tab.id && (
                <motion.div
                  layoutId="tab-underline"
                  className="absolute -bottom-px left-0 right-0 h-0.5 bg-foreground"
                  transition={{ type: "spring", bounce: 0.2, duration: 0.5 }}
                />
              )}
            </button>
          ))}
        </div>
        <div className="mt-4 text-sm leading-relaxed text-muted-foreground">
          {activeTab === "description" ? (
            <p>{product.description || "No description available."}</p>
          ) : (
            <div className="space-y-2">
              <p>Free standard shipping on orders over $50. Express shipping available at checkout.</p>
              <p>{{t.returnsPolicy30d}}</p>
            </div>
          )}
        </div>
      </div>

      {/* Share */}
      <div className="mt-4 flex items-center gap-2">
        <span className="text-xs text-muted-foreground">Share:</span>
        <button
          onClick={() => {
            navigator.clipboard.writeText(window.location.href);
            setLinkCopied(true);
            setTimeout(() => setLinkCopied(false), 2000);
          }}
          className="flex h-8 items-center gap-1.5 rounded-full border border-border/40 px-3 text-xs text-muted-foreground transition-colors hover:text-foreground"
        >
          {linkCopied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
          {linkCopied ? "Copied!" : "Copy Link"}
        </button>
      </div>
    </>
  );
}
