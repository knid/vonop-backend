"use client";

import React, { useState } from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import { Minus, Plus, ShoppingCart, Truck, RotateCcw, Shield, Check, Copy } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { useProduct, useProducts, useCart } from "@/lib/wemob-store";
import { ProductGallery } from "@/components/ecommerce/product-gallery";
import { PriceDisplay } from "@/components/ecommerce/price-display";
import { ReviewList } from "@/components/ecommerce/review-list";
import { WishlistButton } from "@/components/ecommerce/wishlist-button";
import { ProductCard } from "@/components/ecommerce/product-card";
import { Skeleton } from "@/components/ui/skeleton";

export default function ProductDetailPage() {
  const params = useParams();
  const slug = params?.slug as string;
  const { product, isLoading } = useProduct(slug);
  const { products: allProducts } = useProducts();
  const { addItem } = useCart();
  const [quantity, setQuantity] = useState(1);
  const [isAdding, setIsAdding] = useState(false);
  const [addedToCart, setAddedToCart] = useState(false);
  const [activeTab, setActiveTab] = useState<"description" | "shipping">("description");
  const [linkCopied, setLinkCopied] = useState(false);

  const relatedProducts = allProducts?.filter((p) => p.id !== product?.id && p.category === product?.category).slice(0, 4) ?? [];

  const handleAddToCart = async () => {
    if (!product || product.in_stock === false) return;
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

  if (isLoading) {
    return (
      <div className="mx-auto max-w-7xl px-4 py-8 sm:px-6 sm:py-12 lg:px-8">
        <Skeleton className="mb-6 h-5 w-64" />
        <div className="grid gap-8 lg:grid-cols-2">
          <Skeleton className="aspect-square w-full rounded-2xl" />
          <div className="space-y-4">
            <Skeleton className="h-8 w-3/4" />
            <Skeleton className="h-6 w-1/3" />
            <Skeleton className="h-24 w-full" />
            <Skeleton className="h-14 w-full rounded-full" />
          </div>
        </div>
      </div>
    );
  }

  if (!product) {
    return (
      <div className="flex min-h-[40vh] flex-col items-center justify-center px-4 text-center">
        <h1 className="text-2xl font-semibold tracking-tight text-foreground">Product not found</h1>
        <p className="mt-2 text-muted-foreground">The product you are looking for does not exist.</p>
        <Link
          href="/products"
          className="mt-6 inline-flex h-11 items-center rounded-full bg-foreground px-6 text-sm font-medium text-background transition-opacity hover:opacity-90"
        >
          Browse Products
        </Link>
      </div>
    );
  }

  const outOfStock = product.in_stock === false;

  return (
    <div className="mx-auto max-w-7xl px-4 py-8 sm:px-6 sm:py-12 lg:px-8">
      {/* Breadcrumbs */}
      <nav className="mb-6 flex items-center gap-2 text-sm text-muted-foreground/50">
        <Link href="/" className="transition-colors hover:text-foreground">Home</Link>
        <span>/</span>
        <Link href="/products" className="transition-colors hover:text-foreground">Products</Link>
        {product.category && (
          <>
            <span>/</span>
            <Link href={"/categories/" + (product.category_slug || product.category.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/-+$/, ""))} className="transition-colors hover:text-foreground">
              {product.category}
            </Link>
          </>
        )}
        <span>/</span>
        <span className="truncate font-medium text-foreground">{product.name}</span>
      </nav>

      <div className="grid gap-8 lg:grid-cols-2 lg:gap-12">
        <ProductGallery images={product.images || []} productName={product.name} />

        <div className="lg:sticky lg:top-24 lg:self-start">
          {product.category && (
            <p className="mb-2 text-[11px] font-medium uppercase tracking-[0.15em] text-muted-foreground/60">
              {product.category}
            </p>
          )}
          <h1 className="text-3xl font-semibold tracking-tight text-foreground sm:text-4xl">
            {product.name}
          </h1>

          <div className="mt-4 flex items-center gap-3">
            <PriceDisplay product={product} size="lg" />
            <WishlistButton productId={product.id} />
          </div>

          {product.description && (
            <div className="mt-6 text-base font-light leading-relaxed text-muted-foreground">
              <p>{product.description}</p>
            </div>
          )}

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

          {/* Trust Badges */}
          <div className="mt-8 space-y-4">
            <div className="grid grid-cols-3 gap-3">
              <div className="flex flex-col items-center gap-2 rounded-xl border border-border/40 p-3 text-center">
                <Truck className="h-5 w-5 text-muted-foreground" />
                <div>
                  <p className="text-xs font-medium text-foreground">{{t.freeShipping}}</p>
                  <p className="text-[10px] text-muted-foreground">{{t.ordersOver50}}</p>
                </div>
              </div>
              <div className="flex flex-col items-center gap-2 rounded-xl border border-border/40 p-3 text-center">
                <RotateCcw className="h-5 w-5 text-muted-foreground" />
                <div>
                  <p className="text-xs font-medium text-foreground">{{t.easyReturns}}</p>
                  <p className="text-[10px] text-muted-foreground">30-day policy</p>
                </div>
              </div>
              <div className="flex flex-col items-center gap-2 rounded-xl border border-border/40 p-3 text-center">
                <Shield className="h-5 w-5 text-muted-foreground" />
                <div>
                  <p className="text-xs font-medium text-foreground">Secure</p>
                  <p className="text-[10px] text-muted-foreground">SSL encrypted</p>
                </div>
              </div>
            </div>

            {/* {{t.guaranteedSafeCheckout}} */}
            <div className="rounded-xl border border-border/40 p-4">
              <p className="mb-2.5 text-center text-[11px] font-medium uppercase tracking-[0.15em] text-muted-foreground/60">{{t.guaranteedSafeCheckout}}</p>
              <div className="flex items-center justify-center gap-2.5">
                <svg className="h-7 w-auto text-muted-foreground/40" viewBox="0 0 48 32" fill="none"><rect width="48" height="32" rx="4" fill="currentColor" opacity="0.15"/><path d="M19.5 20.5l2-9h3l-2 9h-3zm13-9l-3 6.2-.4-2-1-5.4s-.1-.8-1.2-.8h-4.7l-.1.3s1.4.3 3 1.3l2.3 8.4h3.1l4.7-8h-2.7zm-19 0l-3 9h-3.2l-1.8-7.2c-.1-.4-.2-.6-.5-.7-.6-.3-1.5-.6-2.3-.8l.1-.3h4.8c.6 0 1.2.4 1.3 1.2l1.2 6.2 2.9-7.4h3.5zm26.2 6.2c0-2.5-3.4-2.6-3.4-3.7 0-.3.3-.7 1.1-.8.4 0 1.5-.1 2.7.5l.5-2.2c-.7-.2-1.5-.5-2.6-.5-2.7 0-4.7 1.5-4.7 3.6 0 1.6 1.4 2.4 2.4 3 1.1.5 1.4.8 1.4 1.3 0 .7-.8 1-1.6 1-.7 0-1.9-.3-2.5-.5l-.5 2.2c.6.3 1.6.5 2.7.5 2.9 0 4.8-1.4 4.8-3.7l-.3.3z" fill="currentColor" opacity="0.5"/></svg>
                <svg className="h-7 w-auto text-muted-foreground/40" viewBox="0 0 48 32" fill="none"><rect width="48" height="32" rx="4" fill="currentColor" opacity="0.15"/><circle cx="19" cy="16" r="8" fill="currentColor" opacity="0.2"/><circle cx="29" cy="16" r="8" fill="currentColor" opacity="0.3"/></svg>
                <svg className="h-7 w-auto text-muted-foreground/40" viewBox="0 0 48 32" fill="none"><rect width="48" height="32" rx="4" fill="currentColor" opacity="0.15"/><text x="24" y="20" textAnchor="middle" fontSize="10" fontWeight="bold" fill="currentColor" opacity="0.4">AMEX</text></svg>
                <svg className="h-7 w-auto text-muted-foreground/40" viewBox="0 0 48 32" fill="none"><rect width="48" height="32" rx="4" fill="currentColor" opacity="0.15"/><text x="24" y="19" textAnchor="middle" fontSize="8" fontWeight="600" fontStyle="italic" fill="currentColor" opacity="0.4">PayPal</text></svg>
              </div>
            </div>

            {/* Share */}
            <div className="flex items-center gap-2">
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
          </div>
        </div>
      </div>

      {/* Reviews Section */}
      <div className="mt-20">
        <ReviewList productSlug={product.slug} />
      </div>

      {/* Related Products */}
      {relatedProducts.length > 0 && (
        <div className="mt-20">
          <h2 className="text-2xl font-semibold tracking-tight text-foreground">{{t.youMayAlsoLike}}</h2>
          <div className="mt-8 grid grid-cols-2 gap-6 sm:gap-8 lg:grid-cols-4">
            {relatedProducts.map((p) => (
              <ProductCard key={p.id} product={p} />
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
