"use client";

import React from "react";
import Image from "next/image";
import Link from "next/link";
import { ShoppingBag, ShoppingCart, Heart, Check, Star } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { formatPrice, useCart, type StorefrontProduct } from "@/lib/wemob-store";

interface ProductCardProps {
  product: StorefrontProduct;
  onQuickAdd?: () => void;
}

export function ProductCard({ product, onQuickAdd }: ProductCardProps) {
  const { addItem } = useCart();
  const [isAdding, setIsAdding] = React.useState(false);
  const [addedToCart, setAddedToCart] = React.useState(false);
  const image = product.images?.[0];
  const secondImage = product.images?.[1];
  const onSale = product.compare_at_price > 0 && product.compare_at_price > product.price_in_cents;
  const outOfStock = product.in_stock === false;

  const handleQuickAdd = async (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (outOfStock || addedToCart) return;
    setIsAdding(true);
    try {
      if (onQuickAdd) {
        onQuickAdd();
      } else {
        await addItem(product.id, 1, {
          name: product.name,
          image: image ?? "",
          priceInCents: product.price_in_cents,
        });
      }
      setAddedToCart(true);
      setTimeout(() => setAddedToCart(false), 1500);
    } catch {
      // silently fail — toast handled by parent
    } finally {
      setIsAdding(false);
    }
  };

  return (
    <Link href={"/products/" + product.slug} className="group block">
      <div className="overflow-hidden rounded-2xl shadow-sm shadow-black/5 transition-all duration-300 hover:shadow-lg hover:-translate-y-1">
        <div className="relative aspect-[3/4] overflow-hidden bg-muted">
          {image ? (
            <>
              <Image
                src={image}
                alt={product.name}
                fill
                className="object-cover transition-transform duration-500 group-hover:scale-105"
                sizes="(max-width: 640px) 100vw, (max-width: 1024px) 50vw, 25vw"
                loading="lazy"
              />
              {secondImage && (
                <Image
                  src={secondImage}
                  alt={product.name + " alternate"}
                  fill
                  className="object-cover opacity-0 transition-opacity duration-500 group-hover:opacity-100"
                  sizes="(max-width: 640px) 100vw, (max-width: 1024px) 50vw, 25vw"
                  loading="lazy"
                />
              )}
            </>
          ) : (
            <div className="flex h-full w-full items-center justify-center bg-muted">
              <ShoppingBag className="h-12 w-12 text-muted-foreground/40" />
            </div>
          )}
          {outOfStock && (
            <div className="absolute left-2.5 top-2.5 rounded-full bg-background/90 px-2.5 py-1 text-[11px] font-medium uppercase tracking-[0.15em] text-muted-foreground backdrop-blur-sm">
              {{t.outOfStock}}
            </div>
          )}
          {onSale && !outOfStock && (
            <div className="absolute left-2.5 top-2.5 rounded-full bg-rose-500 px-2.5 py-1 text-[11px] font-medium uppercase tracking-[0.15em] text-white">
              {{t.sale}}
            </div>
          )}
          {!outOfStock && (
            <button
              onClick={handleQuickAdd}
              disabled={isAdding}
              className={"absolute bottom-3 right-3 flex h-10 w-10 items-center justify-center rounded-full shadow-lg transition-all duration-200 opacity-0 translate-y-2 group-hover:opacity-100 group-hover:translate-y-0 disabled:opacity-50 " +
                (addedToCart ? "bg-foreground text-background" : "bg-foreground text-background hover:opacity-90")}
              aria-label={"Add " + product.name + " to cart"}
            >
              <AnimatePresence mode="wait">
                {addedToCart ? (
                  <motion.span key="check" initial={{ scale: 0 }} animate={{ scale: 1 }} exit={{ scale: 0 }}>
                    <Check className="h-4 w-4" />
                  </motion.span>
                ) : (
                  <motion.span key="cart" initial={{ scale: 0 }} animate={{ scale: 1 }} exit={{ scale: 0 }}>
                    <ShoppingCart className="h-4 w-4" />
                  </motion.span>
                )}
              </AnimatePresence>
            </button>
          )}
        </div>
        <div className="p-4 pb-5">
          {product.category && (
            <p className="mb-1 text-[11px] font-medium uppercase tracking-[0.15em] text-muted-foreground/60">
              {product.category}
            </p>
          )}
          <h3 className="truncate text-sm font-semibold text-foreground">{product.name}</h3>
          <div className="mt-1.5 flex items-center gap-2">
            {onSale ? (
              <>
                <span className="text-sm font-semibold text-foreground">{formatPrice(product.price_in_cents)}</span>
                <span className="text-xs text-muted-foreground/60 line-through decoration-muted-foreground/30">{formatPrice(product.compare_at_price)}</span>
              </>
            ) : (
              <span className="text-sm font-semibold text-foreground">{formatPrice(product.price_in_cents)}</span>
            )}
          </div>
          {product.average_rating !== undefined && product.average_rating > 0 && (
            <div className="mt-1.5 flex items-center gap-1 text-xs text-muted-foreground">
              <Star className="h-3 w-3 fill-foreground text-foreground" />
              <span>{product.average_rating.toFixed(1)}</span>
              {product.review_count !== undefined && <span>({product.review_count})</span>}
            </div>
          )}
        </div>
      </div>
    </Link>
  );
}
