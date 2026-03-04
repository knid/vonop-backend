package constants

// ai_ecommerce_templates.go — Pre-built e-commerce React component templates
// Auto-injected for e-commerce projects into components/ecommerce/

const ECOMMERCE_TEMPLATE_PRODUCT_CARD = `"use client";

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
        await addItem(product.id, 1);
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
      <div className="overflow-hidden rounded-2xl shadow-sm shadow-black/5 transition-shadow duration-300 hover:shadow-md">
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
              Out of Stock
            </div>
          )}
          {onSale && !outOfStock && (
            <div className="absolute left-2.5 top-2.5 rounded-full bg-foreground px-2.5 py-1 text-[11px] font-medium uppercase tracking-[0.15em] text-background backdrop-blur-sm">
              Sale
            </div>
          )}
          {!outOfStock && (
            <button
              onClick={handleQuickAdd}
              disabled={isAdding}
              className={"absolute bottom-3 right-3 flex h-10 w-10 items-center justify-center rounded-full shadow-lg transition-all opacity-0 group-hover:opacity-100 disabled:opacity-50 " +
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
`

const ECOMMERCE_TEMPLATE_CART_DRAWER = `"use client";

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
                Cart ({itemCount})
              </h2>
              <button onClick={onClose} className="rounded-full p-1.5 hover:bg-muted" aria-label="Close cart">
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
                  title="Your cart is empty"
                  description="Add some items to get started"
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
                        {formatPrice(shippingRemaining) + " away from free shipping"}
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
                      <span>You qualify for free shipping!</span>
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
                <div className="flex flex-col gap-3">
                  <Link
                    href="/checkout"
                    onClick={onClose}
                    className="flex w-full items-center justify-center rounded-full bg-foreground px-4 py-3 text-sm font-semibold text-background transition-opacity hover:opacity-90"
                  >
                    Checkout
                  </Link>
                  <Link
                    href="/cart"
                    onClick={onClose}
                    className="flex w-full items-center justify-center text-sm font-medium text-muted-foreground underline underline-offset-4 transition-colors hover:text-foreground"
                  >
                    View Cart
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
`

const ECOMMERCE_TEMPLATE_PRICE_DISPLAY = `"use client";

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
      <span className={sizeClasses[size] + " font-semibold " + (onSale ? "text-foreground" : "text-foreground")}>
        {formatPrice(product.price_in_cents)}
      </span>
      {onSale && (
        <>
          <span className={"text-muted-foreground/60 line-through decoration-muted-foreground/30 " + (size === "lg" ? "text-base" : "text-xs")}>
            {formatPrice(product.compare_at_price)}
          </span>
          <span className="text-[11px] font-medium uppercase tracking-[0.15em] text-muted-foreground/60">
            {pct + "% off"}
          </span>
        </>
      )}
      {product.in_stock === false && (
        <span className="rounded-full bg-muted/80 px-2.5 py-0.5 text-[11px] font-medium uppercase tracking-wider text-muted-foreground">
          Out of Stock
        </span>
      )}
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_QUANTITY_SELECTOR = `"use client";

import React, { useState } from "react";
import { Minus, Plus } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";

interface QuantitySelectorProps {
  value?: number;
  onChange?: (value: number) => void;
  min?: number;
  max?: number;
  size?: "sm" | "md";
  disabled?: boolean;
}

export function QuantitySelector({
  value: controlledValue,
  onChange,
  min = 1,
  max = 99,
  size = "md",
  disabled = false,
}: QuantitySelectorProps) {
  const [internalValue, setInternalValue] = useState(controlledValue ?? min);
  const value = controlledValue ?? internalValue;

  const handleChange = (next: number) => {
    setInternalValue(next);
    onChange?.(next);
  };

  const btnSize = size === "sm" ? "h-7 w-7" : "h-9 w-9";
  const textSize = size === "sm" ? "text-sm w-8" : "text-base w-10";
  const iconSize = size === "sm" ? "h-3 w-3" : "h-4 w-4";

  return (
    <div className="inline-flex items-center rounded-xl border border-border/60">
      <button
        onClick={() => handleChange(Math.max(min, value - 1))}
        disabled={disabled || value <= min}
        className={"flex items-center justify-center rounded-l-xl transition-colors hover:bg-muted active:scale-95 disabled:opacity-40 " + btnSize}
        aria-label="Decrease quantity"
      >
        <Minus className={iconSize} />
      </button>
      <div className={"relative overflow-hidden text-center font-medium tabular-nums " + textSize}>
        <AnimatePresence mode="popLayout">
          <motion.span
            key={value}
            initial={{ y: -12, opacity: 0 }}
            animate={{ y: 0, opacity: 1 }}
            exit={{ y: 12, opacity: 0 }}
            transition={{ duration: 0.15 }}
            className="inline-block"
          >
            {value}
          </motion.span>
        </AnimatePresence>
      </div>
      <button
        onClick={() => handleChange(Math.min(max, value + 1))}
        disabled={disabled || value >= max}
        className={"flex items-center justify-center rounded-r-xl transition-colors hover:bg-muted active:scale-95 disabled:opacity-40 " + btnSize}
        aria-label="Increase quantity"
      >
        <Plus className={iconSize} />
      </button>
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_PRODUCT_GALLERY = `"use client";

import React from "react";
import Image from "next/image";
import { ShoppingBag, ChevronLeft, ChevronRight } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";

interface ProductGalleryProps {
  images: string[];
  productName: string;
}

export function ProductGallery({ images, productName }: ProductGalleryProps) {
  const [selectedIndex, setSelectedIndex] = React.useState(0);

  if (!images || images.length === 0) {
    return (
      <div className="flex aspect-square w-full items-center justify-center rounded-2xl bg-muted">
        <ShoppingBag className="h-16 w-16 text-muted-foreground/40" />
      </div>
    );
  }

  const handlePrev = () => setSelectedIndex((i) => (i > 0 ? i - 1 : images.length - 1));
  const handleNext = () => setSelectedIndex((i) => (i < images.length - 1 ? i + 1 : 0));

  return (
    <div className="space-y-3">
      <div className="group relative aspect-square overflow-hidden rounded-2xl bg-muted">
        <AnimatePresence mode="wait">
          <motion.div
            key={selectedIndex}
            initial={{ opacity: 0, scale: 1.02 }}
            animate={{ opacity: 1, scale: 1 }}
            exit={{ opacity: 0, scale: 0.98 }}
            transition={{ duration: 0.4 }}
            className="absolute inset-0"
          >
            <Image
              src={images[selectedIndex]}
              alt={productName + " - Image " + (selectedIndex + 1)}
              fill
              className="object-cover"
              sizes="(max-width: 768px) 100vw, 50vw"
              priority={selectedIndex === 0}
            />
          </motion.div>
        </AnimatePresence>
        {images.length > 1 && (
          <>
            <button
              onClick={handlePrev}
              className="absolute left-3 top-1/2 flex h-10 w-10 -translate-y-1/2 items-center justify-center rounded-full bg-background/90 shadow-sm opacity-0 backdrop-blur-sm transition-opacity group-hover:opacity-100"
              aria-label="Previous image"
            >
              <ChevronLeft className="h-4 w-4" />
            </button>
            <button
              onClick={handleNext}
              className="absolute right-3 top-1/2 flex h-10 w-10 -translate-y-1/2 items-center justify-center rounded-full bg-background/90 shadow-sm opacity-0 backdrop-blur-sm transition-opacity group-hover:opacity-100"
              aria-label="Next image"
            >
              <ChevronRight className="h-4 w-4" />
            </button>
          </>
        )}
      </div>
      {/* Mobile dots */}
      {images.length > 1 && (
        <div className="flex justify-center gap-1.5 sm:hidden">
          {images.map((_, i) => (
            <button
              key={i}
              onClick={() => setSelectedIndex(i)}
              className={"h-1.5 rounded-full transition-all " + (i === selectedIndex ? "w-6 bg-foreground" : "w-1.5 bg-foreground/20")}
              aria-label={"Go to image " + (i + 1)}
            />
          ))}
        </div>
      )}
      {/* Desktop thumbnails */}
      {images.length > 1 && (
        <div className="hidden gap-2 overflow-x-auto pb-1 sm:flex">
          {images.map((img, i) => (
            <button
              key={i}
              onClick={() => setSelectedIndex(i)}
              className={"relative h-20 w-20 flex-shrink-0 overflow-hidden rounded-xl transition-all " +
                (i === selectedIndex ? "ring-2 ring-foreground ring-offset-2" : "opacity-60 hover:opacity-100")}
              aria-label={"View image " + (i + 1)}
            >
              <Image src={img} alt={productName + " thumbnail " + (i + 1)} fill className="object-cover" sizes="80px" />
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_CHECKOUT_FORM = `"use client";

import React from "react";
import { useRouter } from "next/navigation";
import { Loader2, AlertTriangle, Check } from "lucide-react";
import { useCart, useStoreConfig, renderPaymentForm, formatPrice, type CheckoutParams, type ShippingAddress } from "@/lib/wemob-store";
import { OrderSummary } from "@/components/ecommerce/order-summary";

export function CheckoutForm() {
  const router = useRouter();
  const { cart, checkout: doCheckout, isLoading: cartLoading } = useCart();
  const { config, isLoading: configLoading } = useStoreConfig();
  const [isSubmitting, setIsSubmitting] = React.useState(false);
  const [error, setError] = React.useState("");
  const [selectedProvider, setSelectedProvider] = React.useState("");
  const paymentFormRef = React.useRef<HTMLDivElement>(null);

  const [form, setForm] = React.useState({
    email: "",
    name: "",
    line1: "",
    line2: "",
    city: "",
    state: "",
    postal_code: "",
    country: "",
  });

  const providers = config?.payment_providers || [];

  React.useEffect(() => {
    if (providers.length === 1) setSelectedProvider(providers[0]);
  }, [providers.length]);

  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setForm((prev) => ({ ...prev, [e.target.name]: e.target.value }));
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setIsSubmitting(true);

    const shippingAddress: ShippingAddress = {
      line1: form.line1,
      line2: form.line2 || undefined,
      city: form.city,
      state: form.state,
      postal_code: form.postal_code,
      country: form.country,
    };

    try {
      const result = await doCheckout({
        sessionId: "",
        customerEmail: form.email,
        customerName: form.name,
        shippingAddress,
        paymentProvider: selectedProvider || "cod",
      });

      if (result.client_secret && paymentFormRef.current) {
        renderPaymentForm(paymentFormRef.current, result.client_secret);
        return;
      }

      if (!result.payment_url) {
        router.push("/checkout/success?order_id=" + result.order_id);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Checkout failed. Please try again.");
    } finally {
      setIsSubmitting(false);
    }
  };

  if (cartLoading || configLoading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (!cart || cart.items.length === 0) {
    return (
      <div className="py-12 text-center">
        <p className="text-muted-foreground">Your cart is empty. Add items before checking out.</p>
      </div>
    );
  }

  const inputClass = "w-full rounded-xl border border-border/50 bg-transparent px-4 py-2.5 text-sm outline-none transition-colors focus:border-foreground";

  return (
    <div className="grid gap-8 lg:grid-cols-[1fr_400px]">
      <form onSubmit={handleSubmit} className="space-y-6">
        {/* Step indicator */}
        <div className="flex items-center justify-center gap-0">
          {["Information", "Shipping", "Payment"].map((step, i) => (
            <React.Fragment key={step}>
              {i > 0 && <div className={"mx-2 h-px w-8 sm:mx-3 sm:w-12 " + (i === 0 ? "bg-foreground" : "bg-border/40")} />}
              <div className="flex items-center gap-1.5">
                <div className={"flex h-7 w-7 items-center justify-center rounded-full text-xs font-medium " +
                  (i === 0 ? "bg-foreground text-background" : "border border-border/40 text-muted-foreground/60")}>
                  {i + 1}
                </div>
                <span className={"hidden text-sm sm:block " + (i === 0 ? "font-medium text-foreground" : "text-muted-foreground/60")}>
                  {step}
                </span>
              </div>
            </React.Fragment>
          ))}
        </div>

        <div className="rounded-2xl bg-muted/20 p-7">
          <h2 className="mb-5 text-xs font-medium uppercase tracking-wider text-muted-foreground">Contact Information</h2>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="sm:col-span-2">
              <label htmlFor="email" className="mb-1.5 block text-xs uppercase tracking-wider text-muted-foreground">Email</label>
              <input id="email" name="email" type="email" required value={form.email} onChange={handleChange} className={inputClass} />
            </div>
            <div className="sm:col-span-2">
              <label htmlFor="name" className="mb-1.5 block text-xs uppercase tracking-wider text-muted-foreground">Full Name</label>
              <input id="name" name="name" type="text" required value={form.name} onChange={handleChange} className={inputClass} />
            </div>
          </div>
        </div>

        <div className="rounded-2xl bg-muted/20 p-7">
          <h2 className="mb-5 text-xs font-medium uppercase tracking-wider text-muted-foreground">Shipping Address</h2>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="sm:col-span-2">
              <label htmlFor="line1" className="mb-1.5 block text-xs uppercase tracking-wider text-muted-foreground">Address Line 1</label>
              <input id="line1" name="line1" type="text" required value={form.line1} onChange={handleChange} className={inputClass} />
            </div>
            <div className="sm:col-span-2">
              <label htmlFor="line2" className="mb-1.5 block text-xs uppercase tracking-wider text-muted-foreground">Address Line 2 (Optional)</label>
              <input id="line2" name="line2" type="text" value={form.line2} onChange={handleChange} className={inputClass} />
            </div>
            <div>
              <label htmlFor="city" className="mb-1.5 block text-xs uppercase tracking-wider text-muted-foreground">City</label>
              <input id="city" name="city" type="text" required value={form.city} onChange={handleChange} className={inputClass} />
            </div>
            <div>
              <label htmlFor="state" className="mb-1.5 block text-xs uppercase tracking-wider text-muted-foreground">State / Province</label>
              <input id="state" name="state" type="text" required value={form.state} onChange={handleChange} className={inputClass} />
            </div>
            <div>
              <label htmlFor="postal_code" className="mb-1.5 block text-xs uppercase tracking-wider text-muted-foreground">Postal Code</label>
              <input id="postal_code" name="postal_code" type="text" required value={form.postal_code} onChange={handleChange} className={inputClass} />
            </div>
            <div>
              <label htmlFor="country" className="mb-1.5 block text-xs uppercase tracking-wider text-muted-foreground">Country</label>
              <input id="country" name="country" type="text" required value={form.country} onChange={handleChange} className={inputClass} />
            </div>
          </div>
        </div>

        {providers.length > 0 ? (
          <div className="rounded-2xl bg-muted/20 p-7">
            <h2 className="mb-5 text-xs font-medium uppercase tracking-wider text-muted-foreground">Payment Method</h2>
            <div className="space-y-3">
              {providers.map((p) => (
                <label key={p} className={"flex cursor-pointer items-center gap-3 rounded-xl border p-4 transition-colors " + (selectedProvider === p ? "border-foreground bg-foreground/5" : "border-border/40 hover:bg-muted/30")}>
                  <input type="radio" name="payment" value={p} checked={selectedProvider === p} onChange={() => setSelectedProvider(p)} className="h-4 w-4 accent-foreground" />
                  <span className="text-sm font-medium capitalize">{p === "cod" ? "Cash on Delivery" : p}</span>
                </label>
              ))}
            </div>
          </div>
        ) : (
          <div className="flex items-center gap-3 rounded-xl border border-amber-200 bg-amber-50 p-4 dark:border-amber-800 dark:bg-amber-950/30">
            <AlertTriangle className="h-5 w-5 flex-shrink-0 text-amber-600" />
            <p className="text-sm text-amber-800 dark:text-amber-200">
              Payment not yet configured — contact the store owner.
            </p>
          </div>
        )}

        {error && (
          <div className="rounded-xl bg-destructive/10 px-4 py-3 text-sm text-destructive">{error}</div>
        )}

        <button
          type="submit"
          disabled={isSubmitting || (!selectedProvider && providers.length > 0)}
          className="flex w-full items-center justify-center gap-2 rounded-full bg-foreground px-6 py-3.5 text-sm font-semibold tracking-wide text-background transition-opacity hover:opacity-90 disabled:opacity-50"
        >
          {isSubmitting && <Loader2 className="h-4 w-4 animate-spin" />}
          {isSubmitting ? "Processing..." : "Place Order" + (cart ? " · " + formatPrice(cart.total_cents) : "")}
        </button>

        <div ref={paymentFormRef} />
      </form>

      <div className="lg:sticky lg:top-4 lg:self-start">
        <OrderSummary cart={cart} />
      </div>
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_CATEGORY_NAV = `"use client";

import React from "react";
import { motion } from "framer-motion";
import { useCategories, type StorefrontCategory } from "@/lib/wemob-store";

interface CategoryNavProps {
  selectedCategory?: string;
  onSelect: (slug: string | undefined) => void;
}

export function CategoryNav({ selectedCategory, onSelect }: CategoryNavProps) {
  const { categories, isLoading } = useCategories();

  if (isLoading) {
    return (
      <div className="flex gap-2 overflow-x-auto pb-2">
        {[1, 2, 3, 4].map((i) => (
          <div key={i} className="h-9 w-24 animate-pulse rounded-full bg-muted" />
        ))}
      </div>
    );
  }

  if (!categories || categories.length === 0) return null;

  const allItems = [
    { id: "all", slug: undefined as string | undefined, name: "All" },
    ...categories.map((cat) => ({ id: cat.id, slug: cat.slug as string | undefined, name: cat.name })),
  ];

  return (
    <div className="flex flex-wrap gap-2">
      {allItems.map((item) => {
        const isActive = item.slug === undefined ? !selectedCategory : selectedCategory === item.slug;
        return (
          <button
            key={item.id}
            onClick={() => onSelect(item.slug)}
            className={"relative rounded-full px-4 py-2 text-sm font-medium transition-colors " +
              (isActive ? "bg-foreground text-background" : "border border-border/60 text-muted-foreground hover:text-foreground")}
          >
            {isActive && (
              <motion.div
                layoutId="category-pill"
                className="absolute inset-0 rounded-full bg-foreground"
                transition={{ type: "spring", bounce: 0.2, duration: 0.5 }}
              />
            )}
            <span className="relative z-10">{item.name}</span>
          </button>
        );
      })}
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_CART_ITEM_ROW = `"use client";

import React from "react";
import Image from "next/image";
import { Trash2, ShoppingBag } from "lucide-react";
import { motion } from "framer-motion";
import { useCart, formatPrice, type StorefrontCartItem } from "@/lib/wemob-store";
import { QuantitySelector } from "@/components/ecommerce/quantity-selector";

interface CartItemRowProps {
  item: StorefrontCartItem;
  compact?: boolean;
}

export function CartItemRow({ item, compact = false }: CartItemRowProps) {
  const { updateItem, removeItem } = useCart();
  const [isRemoving, setIsRemoving] = React.useState(false);

  const handleQuantityChange = async (qty: number) => {
    try {
      await updateItem(item.id, qty);
    } catch {
      // silently handle — optimistic update will rollback
    }
  };

  const handleRemove = async () => {
    setIsRemoving(true);
    try {
      await removeItem(item.id);
    } catch {
      setIsRemoving(false);
    }
  };

  return (
    <motion.div
      layout
      exit={{ opacity: 0, x: -20 }}
      transition={{ duration: 0.2 }}
      className={"group flex gap-3 " + (compact ? "rounded-xl p-2" : "rounded-xl border border-border/40 p-4 hover:border-border/70 transition-colors") + (isRemoving ? " opacity-50" : "")}
    >
      <div className={"relative flex-shrink-0 overflow-hidden rounded-xl bg-muted " + (compact ? "h-16 w-16" : "h-20 w-20")}>
        {item.product_image ? (
          <Image src={item.product_image} alt={item.product_name} fill className="object-cover" sizes={compact ? "64px" : "80px"} />
        ) : (
          <div className="flex h-full w-full items-center justify-center">
            <ShoppingBag className="h-5 w-5 text-muted-foreground/40" />
          </div>
        )}
      </div>
      <div className="flex flex-1 flex-col justify-between">
        <div>
          <h3 className={"font-medium " + (compact ? "text-sm" : "text-base")}>{item.product_name}</h3>
        </div>
        <div className="flex items-center justify-between">
          <QuantitySelector value={item.quantity} onChange={handleQuantityChange} size={compact ? "sm" : "md"} />
          <div className="flex items-center gap-3">
            <span className={"font-semibold " + (compact ? "text-sm" : "text-base")}>{formatPrice(item.total_cents)}</span>
            <button onClick={handleRemove} disabled={isRemoving} className="text-muted-foreground opacity-0 transition-all hover:text-destructive group-hover:opacity-100" aria-label={"Remove " + item.product_name}>
              <Trash2 className={compact ? "h-3.5 w-3.5" : "h-4 w-4"} />
            </button>
          </div>
        </div>
      </div>
    </motion.div>
  );
}
`

const ECOMMERCE_TEMPLATE_ORDER_SUMMARY = `"use client";

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
        Order Summary
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
          <span className="text-muted-foreground">Shipping</span>
          <span className="text-muted-foreground">Calculated at checkout</span>
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
`

const ECOMMERCE_TEMPLATE_EMPTY_STATE = `"use client";

import React from "react";
import { motion } from "framer-motion";

interface EmptyStateProps {
  icon?: React.ReactNode;
  title: string;
  description?: string;
  action?: React.ReactNode;
}

export function EmptyState({ icon, title, description, action }: EmptyStateProps) {
  return (
    <motion.div
      initial={{ opacity: 0, y: 16 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.4 }}
      className="flex flex-col items-center justify-center py-20 text-center"
    >
      {icon && (
        <div className="mb-5 flex h-20 w-20 items-center justify-center rounded-full bg-muted/50 text-muted-foreground/40">
          {icon}
        </div>
      )}
      <h3 className="mb-1.5 text-lg font-semibold tracking-tight text-foreground">{title}</h3>
      {description && <p className="mb-5 max-w-sm text-sm text-muted-foreground">{description}</p>}
      {action && <div>{action}</div>}
    </motion.div>
  );
}
`

const ECOMMERCE_TEMPLATE_REVIEW_LIST = `"use client";

import React from "react";
import { Star, Loader2 } from "lucide-react";
import { motion } from "framer-motion";
import { useReviews } from "@/lib/wemob-store";

interface ReviewListProps {
  productSlug: string;
}

function StarRating({ rating, size = "sm" }: { rating: number; size?: "sm" | "md" }) {
  const starSize = size === "sm" ? "h-3.5 w-3.5" : "h-5 w-5";
  return (
    <div className="flex gap-0.5">
      {[1, 2, 3, 4, 5].map((i) => (
        <Star
          key={i}
          className={starSize + " " + (i <= rating ? "fill-foreground text-foreground" : "fill-muted/40 text-muted/40")}
        />
      ))}
    </div>
  );
}

function StarInput({ value, onChange }: { value: number; onChange: (v: number) => void }) {
  const [hover, setHover] = React.useState(0);
  return (
    <div className="flex gap-1">
      {[1, 2, 3, 4, 5].map((i) => (
        <button
          key={i}
          type="button"
          onClick={() => onChange(i)}
          onMouseEnter={() => setHover(i)}
          onMouseLeave={() => setHover(0)}
          className="p-0.5"
          aria-label={i + " star" + (i > 1 ? "s" : "")}
        >
          <Star className={"h-6 w-6 " + (i <= (hover || value) ? "fill-foreground text-foreground" : "fill-muted/40 text-muted/40")} />
        </button>
      ))}
    </div>
  );
}

export function ReviewList({ productSlug }: ReviewListProps) {
  const { reviews, averageRating, reviewCount, isLoading, submitReview } = useReviews(productSlug);
  const [showForm, setShowForm] = React.useState(false);
  const [submitting, setSubmitting] = React.useState(false);
  const [submitted, setSubmitted] = React.useState(false);
  const [form, setForm] = React.useState({ name: "", email: "", rating: 5, title: "", body: "" });

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!form.rating || !form.name || !form.email) return;
    setSubmitting(true);
    try {
      await submitReview({ customer_name: form.name, customer_email: form.email, rating: form.rating, title: form.title, body: form.body });
      setSubmitted(true);
      setShowForm(false);
      setForm({ name: "", email: "", rating: 5, title: "", body: "" });
    } catch {
      // silently handle
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-xl font-semibold tracking-tight">Customer Reviews</h3>
          {reviewCount > 0 && (
            <div className="mt-1.5 flex items-center gap-2">
              <StarRating rating={Math.round(averageRating)} size="md" />
              <span className="text-sm text-muted-foreground">
                {averageRating.toFixed(1)} out of 5 ({reviewCount} review{reviewCount !== 1 ? "s" : ""})
              </span>
            </div>
          )}
        </div>
        {!showForm && !submitted && (
          <button onClick={() => setShowForm(true)} className="rounded-full border border-foreground px-5 py-2 text-sm font-medium text-foreground transition-colors hover:bg-foreground hover:text-background">
            Write a Review
          </button>
        )}
      </div>

      {submitted && (
        <div className="rounded-xl bg-green-50 px-4 py-3 text-sm text-green-800 dark:bg-green-950/30 dark:text-green-200">
          Thank you for your review! It has been submitted successfully.
        </div>
      )}

      {showForm && (
        <form onSubmit={handleSubmit} className="space-y-4 rounded-xl bg-muted/20 p-5">
          <div>
            <label className="mb-1.5 block text-xs font-medium uppercase tracking-wider text-muted-foreground">Rating</label>
            <StarInput value={form.rating} onChange={(v) => setForm((f) => ({ ...f, rating: v }))} />
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div>
              <label htmlFor="review-name" className="mb-1.5 block text-xs font-medium uppercase tracking-wider text-muted-foreground">Name</label>
              <input id="review-name" required value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} className="w-full border-b border-border/60 bg-transparent px-0 py-2 text-sm outline-none focus:border-foreground" />
            </div>
            <div>
              <label htmlFor="review-email" className="mb-1.5 block text-xs font-medium uppercase tracking-wider text-muted-foreground">Email</label>
              <input id="review-email" type="email" required value={form.email} onChange={(e) => setForm((f) => ({ ...f, email: e.target.value }))} className="w-full border-b border-border/60 bg-transparent px-0 py-2 text-sm outline-none focus:border-foreground" />
            </div>
          </div>
          <div>
            <label htmlFor="review-title" className="mb-1.5 block text-xs font-medium uppercase tracking-wider text-muted-foreground">Title (optional)</label>
            <input id="review-title" value={form.title} onChange={(e) => setForm((f) => ({ ...f, title: e.target.value }))} className="w-full border-b border-border/60 bg-transparent px-0 py-2 text-sm outline-none focus:border-foreground" />
          </div>
          <div>
            <label htmlFor="review-body" className="mb-1.5 block text-xs font-medium uppercase tracking-wider text-muted-foreground">Review</label>
            <textarea id="review-body" rows={3} value={form.body} onChange={(e) => setForm((f) => ({ ...f, body: e.target.value }))} className="w-full border-b border-border/60 bg-transparent px-0 py-2 text-sm outline-none focus:border-foreground" />
          </div>
          <div className="flex gap-3 pt-2">
            <button type="submit" disabled={submitting} className="flex items-center gap-2 rounded-full bg-foreground px-5 py-2 text-sm font-medium text-background disabled:opacity-50">
              {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
              Submit Review
            </button>
            <button type="button" onClick={() => setShowForm(false)} className="rounded-full px-5 py-2 text-sm font-medium text-muted-foreground hover:text-foreground">
              Cancel
            </button>
          </div>
        </form>
      )}

      {isLoading ? (
        <div className="space-y-4">
          {[1, 2, 3].map((i) => (
            <div key={i} className="h-24 animate-pulse rounded-xl bg-muted" />
          ))}
        </div>
      ) : reviews.length === 0 && !submitted ? (
        <p className="py-8 text-center text-sm text-muted-foreground">No reviews yet. Be the first to review this product!</p>
      ) : (
        <div className="space-y-4">
          {reviews.map((review, idx) => (
            <motion.div
              key={review.id}
              initial={{ opacity: 0, y: 12 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: idx * 0.05 }}
              className="rounded-xl bg-muted/20 p-5"
            >
              <div className="mb-2 flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <StarRating rating={review.rating} />
                  {review.is_verified && (
                    <span className="rounded-full bg-foreground/10 px-2 py-0.5 text-[11px] font-medium uppercase tracking-wider text-foreground/70">Verified</span>
                  )}
                </div>
                <span className="text-xs text-muted-foreground">
                  {new Date(review.created_at).toLocaleDateString()}
                </span>
              </div>
              {review.title && <h4 className="mb-1 font-medium">{review.title}</h4>}
              {review.body && <p className="text-sm text-muted-foreground">{review.body}</p>}
              <p className="mt-2 text-xs text-muted-foreground">by {review.customer_name}</p>
            </motion.div>
          ))}
        </div>
      )}
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_WISHLIST_BUTTON = `"use client";

import React from "react";
import { Heart } from "lucide-react";
import { motion } from "framer-motion";
import { useWishlist } from "@/lib/wemob-store";

interface WishlistButtonProps {
  productId: string;
  size?: "sm" | "md";
  className?: string;
}

export function WishlistButton({ productId, size = "md", className = "" }: WishlistButtonProps) {
  const { isInWishlist, toggleWishlist } = useWishlist();
  const [isToggling, setIsToggling] = React.useState(false);
  const active = isInWishlist(productId);

  const handleClick = async (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (isToggling) return;
    setIsToggling(true);
    try {
      await toggleWishlist(productId);
    } catch {
      // silently handle
    } finally {
      setIsToggling(false);
    }
  };

  const btnSize = size === "sm" ? "h-8 w-8" : "h-10 w-10";
  const iconSize = size === "sm" ? "h-4 w-4" : "h-5 w-5";

  return (
    <motion.button
      onClick={handleClick}
      disabled={isToggling}
      whileTap={{ scale: 0.85 }}
      animate={active ? { scale: [1, 1.15, 1] } : {}}
      transition={{ duration: 0.3 }}
      className={"flex items-center justify-center rounded-full shadow-sm transition-all bg-background/80 backdrop-blur-sm " +
        (active ? "text-red-500" : "text-muted-foreground hover:text-red-500") +
        " " + btnSize + " " + className}
      aria-label={active ? "Remove from wishlist" : "Add to wishlist"}
    >
      <Heart className={iconSize + (active ? " fill-current" : "")} />
    </motion.button>
  );
}
`

const ECOMMERCE_TEMPLATE_SITE_HEADER = `"use client";

import React, { useState, useEffect } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { ShoppingBag, Search, Menu, X } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { useCart, useCategories, useStoreConfig } from "@/lib/wemob-store";
import { CartDrawer } from "@/components/ecommerce/cart-drawer";

export function SiteHeader() {
  const pathname = usePathname();
  const { itemCount } = useCart();
  const { categories } = useCategories();
  const { config: storeConfig } = useStoreConfig();

  const [cartOpen, setCartOpen] = useState(false);
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);
  const [scrolled, setScrolled] = useState(false);

  useEffect(() => {
    const handleScroll = () => setScrolled(window.scrollY > 10);
    window.addEventListener("scroll", handleScroll, { passive: true });
    return () => window.removeEventListener("scroll", handleScroll);
  }, []);

  useEffect(() => {
    setMobileMenuOpen(false);
  }, [pathname]);

  const navLinks = [
    { label: "Home", href: "/" },
    { label: "Products", href: "/products" },
    ...(categories ?? []).map((cat) => ({
      label: cat.name,
      href: "/categories/" + cat.slug,
    })),
    { label: "About", href: "/about" },
    { label: "Contact", href: "/contact" },
  ];

  const isActive = (href: string) =>
    href === "/" ? pathname === "/" : pathname.startsWith(href);

  return (
    <>
      <header
        className={
          "sticky top-0 z-50 w-full transition-all duration-300 " +
          (scrolled
            ? "bg-background/80 backdrop-blur-md border-b border-border/30 shadow-sm"
            : "bg-background")
        }
      >
        <div className="mx-auto flex h-16 max-w-7xl items-center justify-between px-4 sm:px-6 lg:px-8">
          <Link href="/" className="text-xl font-bold tracking-tight text-foreground">
            {storeConfig?.name}
          </Link>

          <nav className="hidden items-center gap-1 md:flex">
            {navLinks.map((link) => (
              <Link
                key={link.href}
                href={link.href}
                className={"relative px-3 py-2 text-sm font-medium transition-colors " +
                  (isActive(link.href) ? "text-foreground" : "text-muted-foreground hover:text-foreground")}
              >
                {link.label}
                {isActive(link.href) && (
                  <motion.div
                    layoutId="nav-underline"
                    className="absolute -bottom-px left-0 right-0 h-0.5 bg-foreground"
                    transition={{ type: "spring", bounce: 0.2, duration: 0.5 }}
                  />
                )}
              </Link>
            ))}
          </nav>

          <div className="flex items-center gap-2">
            <Link
              href="/products?q="
              className="flex h-10 w-10 items-center justify-center rounded-full text-muted-foreground transition-colors hover:text-foreground"
              aria-label="Search products"
            >
              <Search className="h-5 w-5" />
            </Link>

            <button
              onClick={() => setCartOpen(true)}
              className="relative flex h-10 w-10 items-center justify-center rounded-full text-muted-foreground transition-colors hover:text-foreground"
              aria-label="Open cart"
            >
              <ShoppingBag className="h-5 w-5" />
              {itemCount > 0 && (
                <span className="absolute -right-0.5 -top-0.5 flex h-4 w-4 items-center justify-center rounded-full bg-foreground text-[10px] font-semibold text-background">
                  {itemCount > 99 ? "99+" : itemCount}
                </span>
              )}
            </button>

            <button
              onClick={() => setMobileMenuOpen(!mobileMenuOpen)}
              className="flex h-10 w-10 items-center justify-center rounded-full text-muted-foreground transition-colors hover:text-foreground md:hidden"
              aria-label={mobileMenuOpen ? "Close menu" : "Open menu"}
            >
              {mobileMenuOpen ? <X className="h-5 w-5" /> : <Menu className="h-5 w-5" />}
            </button>
          </div>
        </div>
      </header>

      {/* Full-screen mobile menu */}
      <AnimatePresence>
        {mobileMenuOpen && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.2 }}
            className="fixed inset-0 z-40 bg-background md:hidden"
          >
            <div className="flex h-full flex-col items-center justify-center gap-2 px-4">
              {navLinks.map((link, i) => (
                <motion.div
                  key={link.href}
                  initial={{ opacity: 0, y: 20 }}
                  animate={{ opacity: 1, y: 0 }}
                  transition={{ delay: i * 0.05 }}
                >
                  <Link
                    href={link.href}
                    className={"block px-4 py-3 text-center text-2xl font-semibold tracking-tight transition-colors " +
                      (isActive(link.href) ? "text-foreground" : "text-muted-foreground hover:text-foreground")}
                  >
                    {link.label}
                  </Link>
                </motion.div>
              ))}
            </div>
          </motion.div>
        )}
      </AnimatePresence>

      <CartDrawer isOpen={cartOpen} onClose={() => setCartOpen(false)} />
    </>
  );
}
`

const ECOMMERCE_TEMPLATE_SITE_FOOTER = `"use client";

import Link from "next/link";
import { useCategories, useStoreConfig } from "@/lib/wemob-store";

export function SiteFooter() {
  const { categories } = useCategories();
  const { config: storeConfig } = useStoreConfig();

  const shopLinks = [
    { label: "Products", href: "/products" },
    ...(categories ?? []).map((cat) => ({
      label: cat.name,
      href: "/categories/" + cat.slug,
    })),
  ];

  const companyLinks = [
    { label: "About", href: "/about" },
    { label: "Contact", href: "/contact" },
  ];

  const supportLinks = [
    { label: "FAQ", href: "/faq" },
    { label: "Shipping Policy", href: "/policies/shipping" },
    { label: "Returns", href: "/policies/returns" },
  ];

  const legalLinks = [
    { label: "Privacy Policy", href: "/policies/privacy" },
    { label: "Terms of Service", href: "/policies/terms" },
  ];

  return (
    <footer className="border-t border-border/40 bg-background">
      <div className="mx-auto max-w-7xl px-4 py-16 sm:px-6 lg:px-8 lg:py-20">
        <div className="grid grid-cols-1 gap-10 sm:grid-cols-2 lg:grid-cols-4">
          <div className="sm:col-span-2 lg:col-span-1">
            <Link href="/" className="text-lg font-bold tracking-tight text-foreground">
              {storeConfig?.name}
            </Link>
            <p className="mt-3 text-sm leading-relaxed text-muted-foreground">
              Quality products curated for you. Shop with confidence and enjoy a
              seamless shopping experience.
            </p>
            {/* Social icons */}
            <div className="mt-6 flex gap-3">
              {["I", "X", "F"].map((letter) => (
                <a
                  key={letter}
                  href="#"
                  className="flex h-9 w-9 items-center justify-center rounded-full border border-border/40 text-muted-foreground transition-colors hover:border-foreground hover:text-foreground"
                  aria-label={letter === "I" ? "Instagram" : letter === "X" ? "Twitter" : "Facebook"}
                >
                  <span className="text-xs font-medium">{letter}</span>
                </a>
              ))}
            </div>
          </div>

          <div>
            <h3 className="text-[11px] font-medium uppercase tracking-[0.2em] text-muted-foreground/50">
              Shop
            </h3>
            <ul className="mt-4 space-y-2.5">
              {shopLinks.map((link) => (
                <li key={link.href}>
                  <Link
                    href={link.href}
                    className="text-sm text-muted-foreground transition-colors hover:text-foreground"
                  >
                    {link.label}
                  </Link>
                </li>
              ))}
            </ul>
          </div>

          <div>
            <h3 className="text-[11px] font-medium uppercase tracking-[0.2em] text-muted-foreground/50">
              Company
            </h3>
            <ul className="mt-4 space-y-2.5">
              {companyLinks.map((link) => (
                <li key={link.href}>
                  <Link
                    href={link.href}
                    className="text-sm text-muted-foreground transition-colors hover:text-foreground"
                  >
                    {link.label}
                  </Link>
                </li>
              ))}
            </ul>

            <h3 className="mt-8 text-[11px] font-medium uppercase tracking-[0.2em] text-muted-foreground/50">
              Support
            </h3>
            <ul className="mt-4 space-y-2.5">
              {supportLinks.map((link) => (
                <li key={link.href}>
                  <Link
                    href={link.href}
                    className="text-sm text-muted-foreground transition-colors hover:text-foreground"
                  >
                    {link.label}
                  </Link>
                </li>
              ))}
            </ul>
          </div>

          <div>
            <h3 className="text-[11px] font-medium uppercase tracking-[0.2em] text-muted-foreground/50">
              Legal
            </h3>
            <ul className="mt-4 space-y-2.5">
              {legalLinks.map((link) => (
                <li key={link.href}>
                  <Link
                    href={link.href}
                    className="text-sm text-muted-foreground transition-colors hover:text-foreground"
                  >
                    {link.label}
                  </Link>
                </li>
              ))}
            </ul>
          </div>
        </div>

        <div className="mt-16 border-t border-border/40 pt-8">
          <p className="text-center text-xs text-muted-foreground/60">
            &copy; {new Date().getFullYear()} {storeConfig?.name}. All rights reserved.
          </p>
        </div>
      </div>
    </footer>
  );
}
`

const ECOMMERCE_TEMPLATE_PROVIDERS = `"use client";

import React from "react";
import { CartProvider } from "@/lib/wemob-store";

export function Providers({ children }: { children: React.ReactNode }) {
  return <CartProvider>{children}</CartProvider>;
}
`

const ECOMMERCE_TEMPLATE_NEWSLETTER = `"use client";

import React, { useState } from "react";
import { CheckCircle } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";

export function NewsletterSection() {
  const [email, setEmail] = useState("");
  const [status, setStatus] = useState<"idle" | "loading" | "success" | "error">("idle");
  const [message, setMessage] = useState("");

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();

    if (!email || !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
      setStatus("error");
      setMessage("Please enter a valid email address.");
      return;
    }

    setStatus("loading");
    setMessage("");

    setTimeout(() => {
      setStatus("success");
      setMessage("Thank you for subscribing! Check your inbox for a confirmation.");
      setEmail("");
    }, 1000);
  };

  return (
    <section className="py-20 sm:py-28">
      <div className="mx-auto max-w-2xl px-4 text-center sm:px-6 lg:px-8">
        <motion.div
          initial={{ opacity: 0, y: 20 }}
          whileInView={{ opacity: 1, y: 0 }}
          viewport={{ once: true }}
          transition={{ duration: 0.5 }}
        >
          <h2 className="text-3xl font-semibold tracking-tight text-foreground sm:text-4xl">
            Stay in the Loop
          </h2>
          <p className="mt-3 text-muted-foreground">
            Subscribe for exclusive deals, new arrivals, and style inspiration.
          </p>

          <form onSubmit={handleSubmit} className="mt-8">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-center">
              <input
                type="email"
                value={email}
                onChange={(e) => {
                  setEmail(e.target.value);
                  if (status === "error") setStatus("idle");
                }}
                placeholder="Enter your email"
                className="h-12 w-full rounded-full border border-border/60 bg-background px-5 text-sm text-foreground placeholder:text-muted-foreground focus:border-foreground focus:outline-none sm:max-w-xs"
                disabled={status === "loading"}
              />
              <button
                type="submit"
                disabled={status === "loading"}
                className="inline-flex h-12 items-center justify-center rounded-full bg-foreground px-7 text-sm font-medium text-background transition-opacity hover:opacity-90 disabled:opacity-50"
              >
                {status === "loading" ? "Subscribing..." : "Subscribe"}
              </button>
            </div>
          </form>

          <AnimatePresence mode="wait">
            {status === "success" && (
              <motion.div
                initial={{ opacity: 0, y: 5 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0 }}
                className="mt-4 flex items-center justify-center gap-2 text-sm text-green-600 dark:text-green-400"
              >
                <CheckCircle className="h-4 w-4" />
                {message}
              </motion.div>
            )}
            {status === "error" && (
              <motion.p
                initial={{ opacity: 0, y: 5 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0 }}
                className="mt-4 text-sm text-red-600 dark:text-red-400"
              >
                {message}
              </motion.p>
            )}
          </AnimatePresence>

          <p className="mt-4 text-xs text-muted-foreground/60">
            No spam, ever. Unsubscribe at any time.
          </p>
        </motion.div>
      </div>
    </section>
  );
}
`

const ECOMMERCE_TEMPLATE_CART_PAGE = `"use client";

import React, { useState } from "react";
import Link from "next/link";
import { ShoppingBag, ArrowLeft, Check } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { useCart, formatPrice } from "@/lib/wemob-store";
import { CartItemRow } from "@/components/ecommerce/cart-item-row";
import { OrderSummary } from "@/components/ecommerce/order-summary";
import { EmptyState } from "@/components/ecommerce/empty-state";

// Free shipping threshold in cents — change to match your store
const FREE_SHIPPING_THRESHOLD = 5000;

export default function CartPage() {
  const { cart, isLoading, itemCount, applyDiscount, removeDiscountCode } = useCart();

  const [discountCode, setDiscountCode] = useState("");
  const [discountLoading, setDiscountLoading] = useState(false);
  const [discountError, setDiscountError] = useState("");

  const handleApplyDiscount = async () => {
    if (!discountCode.trim()) return;
    setDiscountLoading(true);
    setDiscountError("");
    try {
      await applyDiscount(discountCode.trim());
    } catch {
      setDiscountError("Invalid or expired discount code.");
    } finally {
      setDiscountLoading(false);
    }
  };

  const shippingProgress = cart ? Math.min(cart.subtotal_cents / FREE_SHIPPING_THRESHOLD, 1) : 0;
  const shippingRemaining = cart ? FREE_SHIPPING_THRESHOLD - cart.subtotal_cents : FREE_SHIPPING_THRESHOLD;

  if (isLoading) {
    return (
      <div className="mx-auto max-w-7xl px-4 py-12 sm:px-6 sm:py-16 lg:px-8">
        <h1 className="text-3xl font-semibold tracking-tight text-foreground sm:text-4xl">Shopping Cart</h1>
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
        <h1 className="text-3xl font-semibold tracking-tight text-foreground sm:text-4xl">Shopping Cart</h1>
        <div className="mt-12">
          <EmptyState
            icon={<ShoppingBag className="h-10 w-10" />}
            title="Your cart is empty"
            description="Looks like you haven't added anything to your cart yet."
            action={<Link href="/products" className="inline-flex h-11 items-center rounded-full bg-foreground px-6 text-sm font-medium text-background transition-opacity hover:opacity-90">Continue Shopping</Link>}
          />
        </div>
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-7xl px-4 py-12 sm:px-6 sm:py-16 lg:px-8">
      <div className="flex items-center justify-between">
        <h1 className="text-3xl font-semibold tracking-tight text-foreground sm:text-4xl">
          Shopping Cart ({itemCount})
        </h1>
        <Link
          href="/products"
          className="inline-flex items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
        >
          <ArrowLeft className="h-4 w-4" />
          Continue Shopping
        </Link>
      </div>

      {/* Free shipping progress */}
      <div className="mt-6">
        {shippingRemaining > 0 ? (
          <>
            <p className="mb-2 text-xs text-muted-foreground">
              {formatPrice(shippingRemaining) + " away from free shipping"}
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
            <span>You qualify for free shipping!</span>
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
                Discount Code
              </label>
              <input
                id="discount"
                type="text"
                value={discountCode}
                onChange={(e) => {
                  setDiscountCode(e.target.value);
                  setDiscountError("");
                }}
                placeholder="Enter code"
                className="h-11 w-full rounded-full border border-border/60 bg-background px-5 text-sm text-foreground placeholder:text-muted-foreground focus:border-foreground focus:outline-none"
              />
              {discountError && (
                <p className="mt-1 text-sm text-red-600 dark:text-red-400">{discountError}</p>
              )}
            </div>
            <button
              onClick={handleApplyDiscount}
              disabled={discountLoading || !discountCode.trim()}
              className="h-11 rounded-full bg-muted px-5 text-sm font-medium text-foreground transition-colors hover:bg-muted/80 disabled:opacity-50"
            >
              {discountLoading ? "Applying..." : "Apply"}
            </button>
          </div>

          {cart.discountCode && (
            <div className="mt-3 flex items-center gap-2 text-sm">
              <span className="rounded-full bg-green-100 px-3 py-0.5 font-medium text-green-700 dark:bg-green-900/30 dark:text-green-400">
                {cart.discountCode}
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

        <div className="mt-8 lg:mt-0">
          <OrderSummary cart={cart} />
          <Link
            href="/checkout"
            className="mt-4 flex h-14 w-full items-center justify-center rounded-full bg-foreground text-sm font-semibold text-background transition-opacity hover:opacity-90"
          >
            Proceed to Checkout
          </Link>
        </div>
      </div>

      {/* You might also like */}
      <div className="mt-20">
        <h2 className="text-xl font-semibold tracking-tight text-foreground">You might also like</h2>
        <div className="mt-6 grid grid-cols-2 gap-6 sm:gap-8 lg:grid-cols-4">
          {[1, 2, 3, 4].map((i) => (
            <div key={i} className="space-y-3">
              <div className="aspect-[3/4] animate-pulse rounded-2xl bg-muted" />
              <div className="h-3 w-2/3 animate-pulse rounded bg-muted" />
              <div className="h-3 w-1/3 animate-pulse rounded bg-muted" />
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_CHECKOUT_PAGE = `"use client";

import React from "react";
import Link from "next/link";
import { ArrowLeft, Check } from "lucide-react";
import { motion } from "framer-motion";
import { CheckoutForm } from "@/components/ecommerce/checkout-form";

export default function CheckoutPage() {
  return (
    <div className="mx-auto max-w-4xl px-4 py-12 sm:px-6 sm:py-16 lg:px-8">
      {/* Step progress */}
      <div className="mb-10 flex items-center justify-center gap-0">
        {["Cart", "Information", "Payment"].map((step, i) => (
          <React.Fragment key={step}>
            {i > 0 && <div className={"mx-2 h-px w-8 sm:mx-3 sm:w-12 " + (i <= 1 ? "bg-foreground" : "bg-border/40")} />}
            <div className="flex items-center gap-1.5">
              <div className={"flex h-7 w-7 items-center justify-center rounded-full text-xs font-medium " +
                (i < 1 ? "bg-foreground/10 text-foreground" : i === 1 ? "bg-foreground text-background" : "border border-border/40 text-muted-foreground/60")}>
                {i < 1 ? <Check className="h-3.5 w-3.5" /> : i + 1}
              </div>
              <span className={"hidden text-sm sm:block " + (i === 1 ? "font-medium text-foreground" : "text-muted-foreground/60")}>
                {step}
              </span>
            </div>
          </React.Fragment>
        ))}
      </div>

      <nav className="mb-6">
        <Link
          href="/cart"
          className="inline-flex items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to Cart
        </Link>
      </nav>

      <h1 className="text-3xl font-semibold tracking-tight text-foreground sm:text-4xl">Checkout</h1>
      <p className="mt-1 text-muted-foreground">
        Complete your order by filling in the details below.
      </p>

      <div className="mt-8">
        <CheckoutForm />
      </div>
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_CHECKOUT_SUCCESS_PAGE = `"use client";

import React, { useEffect, useState } from "react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { CheckCircle, Package, Loader2 } from "lucide-react";
import { motion } from "framer-motion";
import { getOrder, confirmCheckout, formatPrice } from "@/lib/wemob-store";

export default function CheckoutSuccessPage() {
  const searchParams = useSearchParams();
  const orderId = searchParams.get("order_id");
  const sessionId = searchParams.get("session_id");

  const [order, setOrder] = useState<any | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!orderId) {
      setLoading(false);
      setError("No order ID provided.");
      return;
    }

    const loadOrder = async () => {
      try {
        // If redirected from Stripe, confirm the payment server-side first
        if (sessionId) {
          await confirmCheckout(orderId, sessionId).catch(() => {
            // Webhook may have already confirmed — ignore errors
          });
        }
        const data = await getOrder(orderId);
        setOrder(data);
      } catch {
        setError("Order not found.");
      } finally {
        setLoading(false);
      }
    };

    loadOrder();
  }, [orderId, sessionId]);

  if (loading) {
    return (
      <div className="flex min-h-[50vh] flex-col items-center justify-center px-4">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
        <p className="mt-4 text-muted-foreground">Loading order details...</p>
      </div>
    );
  }

  if (error || !order) {
    return (
      <div className="flex min-h-[50vh] flex-col items-center justify-center px-4 text-center">
        <Package className="h-12 w-12 text-muted-foreground" />
        <h1 className="mt-4 text-xl font-semibold tracking-tight text-foreground">
          {error || "Order not found"}
        </h1>
        <p className="mt-2 text-muted-foreground">
          We couldn&apos;t find this order. It may have expired or the link is invalid.
        </p>
        <Link
          href="/products"
          className="mt-6 inline-flex h-10 items-center rounded-full bg-foreground px-6 text-sm font-medium text-background transition-opacity hover:opacity-90"
        >
          Continue Shopping
        </Link>
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-lg px-4 py-16 text-center sm:px-6 sm:py-20">
      <motion.div
        initial={{ scale: 0 }}
        animate={{ scale: 1 }}
        transition={{ type: "spring", stiffness: 200, damping: 15 }}
        className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-green-100 dark:bg-green-900/30"
      >
        <CheckCircle className="h-8 w-8 text-green-600 dark:text-green-400" />
      </motion.div>

      <h1 className="mt-6 text-3xl font-semibold tracking-tight text-foreground">
        Order Confirmed!
      </h1>
      <p className="mt-2 text-muted-foreground">
        Thank you for your purchase. Your order number is{" "}
        <span className="font-semibold text-foreground">{order.order_number || orderId}</span>.
      </p>

      <div className="mt-8 rounded-xl bg-muted/30 p-6 text-left">
        <h2 className="text-sm font-medium uppercase tracking-[0.15em] text-muted-foreground">
          Order Summary
        </h2>
        <ul className="mt-4 divide-y divide-border/40">
          {order.items?.map((item: any, idx: number) => (
            <motion.li
              key={idx}
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: idx * 0.05 }}
              className="flex items-center justify-between py-3"
            >
              <div>
                <p className="text-sm font-medium text-foreground">{item.product_name}</p>
                <p className="text-xs text-muted-foreground">Qty: {item.quantity}</p>
              </div>
              <p className="text-sm font-medium text-foreground">
                {formatPrice(item.total_cents)}
              </p>
            </motion.li>
          ))}
        </ul>
        <div className="mt-4 flex items-center justify-between border-t border-border/40 pt-4">
          <span className="font-semibold text-foreground">Total</span>
          <span className="text-lg font-semibold tracking-tight text-foreground">
            {formatPrice(order.total_cents)}
          </span>
        </div>
      </div>

      <Link
        href="/products"
        className="mt-8 inline-flex h-11 items-center rounded-full bg-foreground px-6 text-sm font-medium text-background transition-opacity hover:opacity-90"
      >
        Continue Shopping
      </Link>
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_CHECKOUT_CANCEL_PAGE = `"use client";

import Link from "next/link";
import { XCircle } from "lucide-react";
import { motion } from "framer-motion";

export default function CheckoutCancelPage() {
  return (
    <div className="flex min-h-[50vh] flex-col items-center justify-center px-4 py-16 text-center">
      <motion.div
        initial={{ scale: 0 }}
        animate={{ scale: 1 }}
        transition={{ type: "spring", stiffness: 200, damping: 15 }}
        className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-muted/50"
      >
        <XCircle className="h-8 w-8 text-muted-foreground" />
      </motion.div>

      <h1 className="mt-6 text-2xl font-semibold tracking-tight text-foreground">
        Payment Cancelled
      </h1>
      <p className="mx-auto mt-2 max-w-md text-muted-foreground">
        Your payment was not completed. You can try again or continue shopping.
      </p>

      <div className="mt-8 flex flex-col gap-3 sm:flex-row">
        <Link
          href="/checkout"
          className="inline-flex h-11 items-center justify-center rounded-full bg-foreground px-6 text-sm font-medium text-background transition-opacity hover:opacity-90"
        >
          Try Again
        </Link>
        <Link
          href="/products"
          className="inline-flex h-11 items-center justify-center rounded-full border border-border/60 px-6 text-sm font-medium text-foreground transition-colors hover:bg-muted"
        >
          Continue Shopping
        </Link>
      </div>
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_PRODUCTS_PAGE = `"use client";

import React, { useState, useMemo } from "react";
import Link from "next/link";
import { Search, SlidersHorizontal, ChevronDown } from "lucide-react";
import { motion } from "framer-motion";
import { useProducts, useCategories } from "@/lib/wemob-store";
import { ProductCard } from "@/components/ecommerce/product-card";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/components/ecommerce/empty-state";

type SortOption = "newest" | "price-asc" | "price-desc" | "name";

const containerVariants = {
  hidden: { opacity: 0 },
  show: { opacity: 1, transition: { staggerChildren: 0.06 } },
};
const itemVariants = {
  hidden: { opacity: 0, y: 20 },
  show: { opacity: 1, y: 0 },
};

export default function ProductsPage() {
  const { products, isLoading } = useProducts();
  const { categories } = useCategories();
  const [search, setSearch] = useState("");
  const [selectedCategory, setSelectedCategory] = useState<string | null>(null);
  const [sort, setSort] = useState<SortOption>("newest");
  const [showSort, setShowSort] = useState(false);

  const sortLabels: Record<SortOption, string> = {
    newest: "Newest",
    "price-asc": "Price: Low to High",
    "price-desc": "Price: High to Low",
    name: "Name A\u2013Z",
  };

  const filtered = useMemo(() => {
    let result = products ?? [];

    if (search.trim()) {
      const q = search.toLowerCase();
      result = result.filter(
        (p) =>
          p.name.toLowerCase().includes(q) ||
          (p.description && p.description.toLowerCase().includes(q))
      );
    }

    if (selectedCategory) {
      const catName = categories?.find((c) => c.slug === selectedCategory)?.name;
      result = result.filter((p) => p.category === catName);
    }

    switch (sort) {
      case "price-asc":
        result = [...result].sort((a, b) => a.price_in_cents - b.price_in_cents);
        break;
      case "price-desc":
        result = [...result].sort((a, b) => b.price_in_cents - a.price_in_cents);
        break;
      case "name":
        result = [...result].sort((a, b) => a.name.localeCompare(b.name));
        break;
      default:
        break;
    }

    return result;
  }, [products, categories, search, selectedCategory, sort]);

  return (
    <div className="mx-auto max-w-7xl px-4 py-12 sm:px-6 sm:py-16 lg:px-8">
      <div className="mb-10">
        <h1 className="text-4xl font-semibold tracking-tight text-foreground sm:text-5xl">
          All Products
        </h1>
        <p className="mt-3 text-muted-foreground">
          Browse our complete collection
        </p>
      </div>

      {/* Search & Sort Bar */}
      <div className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="relative flex-1 sm:max-w-sm">
          <Search className="absolute left-4 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search products..."
            className="h-11 w-full rounded-full border border-border/60 bg-background pl-11 pr-5 text-sm text-foreground placeholder:text-muted-foreground focus:border-foreground focus:outline-none"
          />
        </div>

        <div className="relative">
          <button
            onClick={() => setShowSort(!showSort)}
            className="inline-flex h-11 items-center gap-2 rounded-full border border-border/60 bg-background px-5 text-sm font-medium text-foreground transition-colors hover:bg-muted"
          >
            <SlidersHorizontal className="h-4 w-4" />
            {sortLabels[sort]}
            <ChevronDown className="h-4 w-4" />
          </button>
          {showSort && (
            <div className="absolute right-0 top-full z-20 mt-1 w-48 rounded-xl border border-border/40 bg-background py-1 shadow-lg">
              {(Object.entries(sortLabels) as [SortOption, string][]).map(([key, label]) => (
                <button
                  key={key}
                  onClick={() => {
                    setSort(key);
                    setShowSort(false);
                  }}
                  className={"w-full px-4 py-2 text-left text-sm transition-colors hover:bg-muted " + (sort === key ? "font-medium text-foreground" : "text-muted-foreground")}
                >
                  {label}
                </button>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* Category Filter Pills */}
      {categories && categories.length > 0 && (
        <div className="mb-8 flex flex-wrap gap-2">
          <button
            onClick={() => setSelectedCategory(null)}
            className={"rounded-full px-4 py-1.5 text-sm font-medium transition-colors " + (selectedCategory === null ? "bg-foreground text-background" : "border border-border/60 text-muted-foreground hover:text-foreground")}
          >
            All
          </button>
          {categories.map((cat) => (
            <button
              key={cat.id}
              onClick={() => setSelectedCategory(selectedCategory === cat.slug ? null : cat.slug)}
              className={"rounded-full px-4 py-1.5 text-sm font-medium transition-colors " + (selectedCategory === cat.slug ? "bg-foreground text-background" : "border border-border/60 text-muted-foreground hover:text-foreground")}
            >
              {cat.name}
            </button>
          ))}
        </div>
      )}

      {/* Product Grid */}
      {isLoading ? (
        <div className="grid grid-cols-2 gap-6 sm:gap-8 lg:grid-cols-3 xl:grid-cols-4">
          {Array.from({ length: 8 }).map((_, i) => (
            <div key={i} className="space-y-3">
              <Skeleton className="aspect-[3/4] w-full rounded-2xl" />
              <Skeleton className="h-4 w-3/4" />
              <Skeleton className="h-4 w-1/2" />
            </div>
          ))}
        </div>
      ) : filtered.length === 0 ? (
        <EmptyState
          icon={<Search className="h-10 w-10" />}
          title="No products found"
          description={search ? "Try adjusting your search or filters." : "Check back soon for new arrivals."}
          action={(search || selectedCategory) ? <Link href="/products" className="inline-flex h-11 items-center rounded-full bg-foreground px-6 text-sm font-medium text-background transition-opacity hover:opacity-90">Clear Filters</Link> : undefined}
        />
      ) : (
        <motion.div
          variants={containerVariants}
          initial="hidden"
          animate="show"
          className="grid grid-cols-2 gap-6 sm:gap-8 lg:grid-cols-3 xl:grid-cols-4"
        >
          {filtered.map((product) => (
            <motion.div key={product.id} variants={itemVariants}>
              <ProductCard product={product} />
            </motion.div>
          ))}
        </motion.div>
      )}
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_PRODUCT_DETAIL_PAGE = `"use client";

import React, { useState } from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import { Minus, Plus, ShoppingCart, Truck, RotateCcw, Shield, Check } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { useProduct, useCart, formatPrice } from "@/lib/wemob-store";
import { ProductGallery } from "@/components/ecommerce/product-gallery";
import { PriceDisplay } from "@/components/ecommerce/price-display";
import { ReviewList } from "@/components/ecommerce/review-list";
import { WishlistButton } from "@/components/ecommerce/wishlist-button";
import { Skeleton } from "@/components/ui/skeleton";

export default function ProductDetailPage() {
  const params = useParams();
  const slug = params?.slug as string;
  const { product, isLoading } = useProduct(slug);
  const { addItem } = useCart();
  const [quantity, setQuantity] = useState(1);
  const [isAdding, setIsAdding] = useState(false);
  const [addedToCart, setAddedToCart] = useState(false);
  const [activeTab, setActiveTab] = useState<"description" | "shipping">("description");

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
            <Link href={"/categories/" + product.category.toLowerCase().replace(/\s+/g, "-")} className="transition-colors hover:text-foreground">
              {product.category}
            </Link>
          </>
        )}
        <span>/</span>
        <span className="truncate font-medium text-foreground">{product.name}</span>
      </nav>

      <div className="grid gap-8 lg:grid-cols-2 lg:gap-12">
        <ProductGallery images={product.images || []} productName={product.name} />

        <div>
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
                <span className="text-sm font-medium text-foreground">Quantity</span>
                <div className="flex items-center rounded-xl border border-border/60">
                  <button
                    onClick={() => setQuantity(Math.max(1, quantity - 1))}
                    className="flex h-10 w-10 items-center justify-center text-muted-foreground transition-colors hover:text-foreground active:scale-95"
                    aria-label="Decrease quantity"
                  >
                    <Minus className="h-4 w-4" />
                  </button>
                  <span className="flex h-10 w-12 items-center justify-center text-sm font-medium tabular-nums text-foreground">
                    {quantity}
                  </span>
                  <button
                    onClick={() => setQuantity(quantity + 1)}
                    className="flex h-10 w-10 items-center justify-center text-muted-foreground transition-colors hover:text-foreground active:scale-95"
                    aria-label="Increase quantity"
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
                    <Check className="h-4 w-4" /> Added to Cart
                  </motion.span>
                ) : (
                  <motion.span key="add" initial={{ scale: 0.9, opacity: 0 }} animate={{ scale: 1, opacity: 1 }} className="flex items-center gap-2">
                    <ShoppingCart className="h-4 w-4" />
                    {isAdding ? "Adding..." : "Add to Cart"}
                  </motion.span>
                )}
              </AnimatePresence>
            </button>
          </div>

          {/* Info Tabs */}
          <div className="mt-10 border-t border-border/40 pt-6">
            <div className="flex gap-6">
              {[{ id: "description" as const, label: "Description" }, { id: "shipping" as const, label: "Shipping & Returns" }].map((tab) => (
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
                  <p>Returns accepted within 30 days of delivery. Items must be unused and in original packaging.</p>
                </div>
              )}
            </div>
          </div>

          {/* Trust Badges */}
          <div className="mt-8 rounded-xl bg-muted/30 p-5">
            <div className="grid grid-cols-3 gap-4">
              <div className="flex flex-col items-center gap-1.5 text-center">
                <Truck className="h-5 w-5 text-muted-foreground" />
                <span className="text-xs text-muted-foreground">Free Shipping</span>
              </div>
              <div className="flex flex-col items-center gap-1.5 text-center">
                <RotateCcw className="h-5 w-5 text-muted-foreground" />
                <span className="text-xs text-muted-foreground">Easy Returns</span>
              </div>
              <div className="flex flex-col items-center gap-1.5 text-center">
                <Shield className="h-5 w-5 text-muted-foreground" />
                <span className="text-xs text-muted-foreground">Secure Payment</span>
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* Reviews Section */}
      <div className="mt-20">
        <ReviewList productSlug={product.slug} />
      </div>
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_CATEGORY_PAGE = `"use client";

import React from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import { ShoppingBag } from "lucide-react";
import { motion } from "framer-motion";
import { useProducts, useCategories } from "@/lib/wemob-store";
import { ProductCard } from "@/components/ecommerce/product-card";
import { EmptyState } from "@/components/ecommerce/empty-state";
import { Skeleton } from "@/components/ui/skeleton";

const containerVariants = {
  hidden: { opacity: 0 },
  show: { opacity: 1, transition: { staggerChildren: 0.06 } },
};
const itemVariants = {
  hidden: { opacity: 0, y: 20 },
  show: { opacity: 1, y: 0 },
};

export default function CategoryPage() {
  const params = useParams();
  const slug = params?.slug as string;
  const { products, isLoading: productsLoading } = useProducts();
  const { categories, isLoading: categoriesLoading } = useCategories();

  const isLoading = productsLoading || categoriesLoading;
  const category = categories?.find((c) => c.slug === slug);
  const filtered = products?.filter(
    (p) => p.category === category?.name
  ) ?? [];

  if (isLoading) {
    return (
      <div className="mx-auto max-w-7xl px-4 py-12 sm:px-6 sm:py-16 lg:px-8">
        <Skeleton className="mb-2 h-5 w-48" />
        <Skeleton className="mb-8 h-12 w-64" />
        <div className="grid grid-cols-2 gap-6 sm:gap-8 lg:grid-cols-3 xl:grid-cols-4">
          {Array.from({ length: 8 }).map((_, i) => (
            <div key={i} className="space-y-3">
              <Skeleton className="aspect-[3/4] w-full rounded-2xl" />
              <Skeleton className="h-4 w-3/4" />
              <Skeleton className="h-4 w-1/2" />
            </div>
          ))}
        </div>
      </div>
    );
  }

  const categoryName = category?.name ?? slug.replace(/-/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());

  return (
    <div className="mx-auto max-w-7xl px-4 py-12 sm:px-6 sm:py-16 lg:px-8">
      {/* Breadcrumbs */}
      <nav className="mb-6 flex items-center gap-2 text-sm text-muted-foreground/50">
        <Link href="/" className="transition-colors hover:text-foreground">Home</Link>
        <span>/</span>
        <Link href="/products" className="transition-colors hover:text-foreground">Products</Link>
        <span>/</span>
        <span className="font-medium text-foreground">{categoryName}</span>
      </nav>

      <div className="mb-10">
        <h1 className="text-4xl font-semibold tracking-tight text-foreground sm:text-5xl">
          {categoryName}
        </h1>
        {category?.description && (
          <p className="mt-3 text-muted-foreground">{category.description}</p>
        )}
        <p className="mt-1 text-sm text-muted-foreground">
          {filtered.length} {filtered.length === 1 ? "product" : "products"}
        </p>
      </div>

      {filtered.length === 0 ? (
        <EmptyState
          icon={<ShoppingBag className="h-10 w-10" />}
          title="No products in this category"
          description="Check back soon for new arrivals in this category."
          action={<Link href="/products" className="inline-flex h-11 items-center rounded-full bg-foreground px-6 text-sm font-medium text-background transition-opacity hover:opacity-90">Browse All Products</Link>}
        />
      ) : (
        <motion.div
          variants={containerVariants}
          initial="hidden"
          animate="show"
          className="grid grid-cols-2 gap-6 sm:gap-8 lg:grid-cols-3 xl:grid-cols-4"
        >
          {filtered.map((product) => (
            <motion.div key={product.id} variants={itemVariants}>
              <ProductCard product={product} />
            </motion.div>
          ))}
        </motion.div>
      )}
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_SHIPPING_POLICY = `"use client";

import { Truck, Clock, Globe, MapPin, Package } from "lucide-react";
import { motion } from "framer-motion";
import { useStoreConfig } from "@/lib/wemob-store";

export default function ShippingPolicyPage() {
  const { config } = useStoreConfig();
  const storeName = config?.name || "Our Store";

  const sections = [
    { icon: Clock, title: "Processing Time", content: "All orders placed with " + storeName + " are processed within 1\u20133 business days. Orders placed on weekends or holidays will be processed on the next business day. You will receive a confirmation email with tracking information once your order has shipped." },
    { icon: Globe, title: "International Shipping", content: "We currently ship to select international destinations. International shipping rates and delivery times vary by location and will be calculated at checkout. Please note that international orders may be subject to customs duties and taxes, which are the responsibility of the recipient." },
    { icon: MapPin, title: "Order Tracking", content: "Once your order ships, you will receive an email with a tracking number. You can use this number to track your package on the carrier\u2019s website. If you have any questions about your shipment, please don\u2019t hesitate to contact our support team." },
    { icon: Package, title: "Shipping Restrictions", content: "Some items may have shipping restrictions due to size, weight, or destination regulations. If an item cannot be shipped to your address, you will be notified during checkout. We are unable to ship to P.O. boxes for express and overnight orders." },
  ];

  return (
    <div className="mx-auto max-w-3xl px-4 py-12 sm:px-6 sm:py-16 lg:px-8">
      <h1 className="text-3xl font-semibold tracking-tight text-foreground sm:text-4xl">Shipping Policy</h1>
      <div className="mt-3">
        <span className="inline-flex rounded-full bg-muted/30 px-3 py-1 text-xs text-muted-foreground">
          Last updated: {new Date().toLocaleDateString("en-US", { month: "long", year: "numeric" })}
        </span>
      </div>

      <div className="mt-10 space-y-6">
        {/* Shipping rates table */}
        <motion.div
          initial={{ opacity: 0, y: 20 }}
          whileInView={{ opacity: 1, y: 0 }}
          viewport={{ once: true }}
          className="rounded-2xl bg-muted/20 p-6 sm:p-8"
        >
          <div className="mb-4 flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-muted/50">
              <Truck className="h-5 w-5 text-muted-foreground" />
            </div>
            <h2 className="text-lg font-semibold tracking-tight text-foreground">Shipping Rates & Delivery Times</h2>
          </div>
          <div className="overflow-hidden rounded-xl border border-border/40">
            <table className="w-full text-left text-sm">
              <thead className="bg-muted/30">
                <tr>
                  <th className="px-4 py-3 text-[11px] font-medium uppercase tracking-[0.15em] text-muted-foreground">Method</th>
                  <th className="px-4 py-3 text-[11px] font-medium uppercase tracking-[0.15em] text-muted-foreground">Estimated Delivery</th>
                  <th className="px-4 py-3 text-[11px] font-medium uppercase tracking-[0.15em] text-muted-foreground">Cost</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border/40 text-muted-foreground">
                <tr><td className="px-4 py-3">Standard Shipping</td><td className="px-4 py-3">5\u20137 business days</td><td className="px-4 py-3">Free on orders over $50</td></tr>
                <tr><td className="px-4 py-3">Express Shipping</td><td className="px-4 py-3">2\u20133 business days</td><td className="px-4 py-3">$9.99</td></tr>
                <tr><td className="px-4 py-3">Overnight Shipping</td><td className="px-4 py-3">1 business day</td><td className="px-4 py-3">$19.99</td></tr>
              </tbody>
            </table>
          </div>
        </motion.div>

        {sections.map((section, idx) => (
          <motion.section
            key={section.title}
            initial={{ opacity: 0, y: 20 }}
            whileInView={{ opacity: 1, y: 0 }}
            viewport={{ once: true }}
            transition={{ delay: idx * 0.1 }}
            className="rounded-2xl bg-muted/20 p-6 sm:p-8"
          >
            <div className="mb-4 flex items-center gap-3">
              <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-muted/50">
                <section.icon className="h-5 w-5 text-muted-foreground" />
              </div>
              <h2 className="text-lg font-semibold tracking-tight text-foreground">{section.title}</h2>
            </div>
            <p className="text-sm leading-relaxed text-muted-foreground">{section.content}</p>
          </motion.section>
        ))}
      </div>
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_RETURNS_POLICY = `"use client";

import { RotateCcw, CheckCircle, Package, CreditCard, AlertCircle } from "lucide-react";
import { motion } from "framer-motion";
import { useStoreConfig } from "@/lib/wemob-store";

export default function ReturnsPolicyPage() {
  const { config } = useStoreConfig();
  const storeName = config?.name || "Our Store";

  const sections = [
    { icon: RotateCcw, title: "Our Return Promise", content: "At " + storeName + ", we want you to be completely satisfied with your purchase. If for any reason you are not happy with your order, we offer a hassle-free return and exchange policy within 30 days of delivery." },
    { icon: CheckCircle, title: "Eligibility", items: ["Items must be returned within 30 days of delivery.", "Products must be in their original condition, unworn, unwashed, and with all tags attached.", "Items must be in their original packaging when possible.", "Sale items and gift cards are final sale and cannot be returned.", "Personalized or custom-made items are non-returnable."] },
    { icon: Package, title: "How to Return", items: ["Contact our support team to initiate a return request.", "You will receive a return authorization and prepaid shipping label via email.", "Pack the item securely in its original packaging.", "Attach the shipping label and drop off the package at the designated carrier.", "Once we receive and inspect the item, your refund will be processed within 5\u20137 business days."], numbered: true },
    { icon: RotateCcw, title: "Exchanges", content: "If you would like to exchange an item for a different size or color, please follow the return process above and place a new order for the desired item. This ensures the fastest processing time." },
    { icon: CreditCard, title: "Refunds", content: "Refunds will be issued to the original payment method. Please allow 5\u20137 business days for the refund to appear on your statement after we have received and processed your return. Shipping costs are non-refundable unless the return is due to a defective or incorrect item." },
    { icon: AlertCircle, title: "Damaged or Defective Items", content: "If you received a damaged or defective item, please contact us within 48 hours of delivery with photos of the issue. We will arrange a replacement or full refund at no additional cost to you." },
  ];

  return (
    <div className="mx-auto max-w-3xl px-4 py-12 sm:px-6 sm:py-16 lg:px-8">
      <h1 className="text-3xl font-semibold tracking-tight text-foreground sm:text-4xl">Return & Exchange Policy</h1>
      <div className="mt-3">
        <span className="inline-flex rounded-full bg-muted/30 px-3 py-1 text-xs text-muted-foreground">
          Last updated: {new Date().toLocaleDateString("en-US", { month: "long", year: "numeric" })}
        </span>
      </div>

      <div className="mt-10 space-y-6">
        {sections.map((section, idx) => (
          <motion.section
            key={section.title}
            initial={{ opacity: 0, y: 20 }}
            whileInView={{ opacity: 1, y: 0 }}
            viewport={{ once: true }}
            transition={{ delay: idx * 0.1 }}
            className="rounded-2xl bg-muted/20 p-6 sm:p-8"
          >
            <div className="mb-4 flex items-center gap-3">
              <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-muted/50">
                <section.icon className="h-5 w-5 text-muted-foreground" />
              </div>
              <h2 className="text-lg font-semibold tracking-tight text-foreground">{section.title}</h2>
            </div>
            {section.content && <p className="text-sm leading-relaxed text-muted-foreground">{section.content}</p>}
            {section.items && (
              <div className="space-y-2">
                {section.items.map((item, i) => (
                  <div key={i} className="flex items-start gap-3 text-sm leading-relaxed text-muted-foreground">
                    <span className="mt-0.5 flex h-5 w-5 flex-shrink-0 items-center justify-center rounded-full bg-muted/50 text-[11px] font-medium text-muted-foreground">
                      {section.numbered ? i + 1 : "\u2022"}
                    </span>
                    <span>{item}</span>
                  </div>
                ))}
              </div>
            )}
          </motion.section>
        ))}
      </div>
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_PRIVACY_POLICY = `"use client";

import { Shield, Database, Cookie, Share2, Lock, User } from "lucide-react";
import { motion } from "framer-motion";
import { useStoreConfig } from "@/lib/wemob-store";

export default function PrivacyPolicyPage() {
  const { config } = useStoreConfig();
  const storeName = config?.name || "Our Store";

  const sections = [
    { icon: Shield, title: "Introduction", content: storeName + " (\u201Cwe\u201D, \u201Cour\u201D, or \u201Cus\u201D) is committed to protecting your privacy. This Privacy Policy explains how we collect, use, disclose, and safeguard your information when you visit our website and make purchases from our store." },
    { icon: Database, title: "Information We Collect", content: "We may collect information about you in a variety of ways, including:", items: ["Personal Data: Name, email address, shipping address, phone number, and payment information provided when you place an order.", "Order Data: Products purchased, order history, and transaction details.", "Usage Data: Browser type, IP address, pages visited, and time spent on our website, collected automatically through cookies and similar technologies.", "Communication Data: Information you provide when contacting our support team or subscribing to our newsletter."] },
    { icon: User, title: "How We Use Your Information", items: ["Process and fulfill your orders, including shipping and payment processing.", "Send order confirmations, shipping updates, and receipts.", "Respond to your inquiries and provide customer support.", "Send promotional emails and newsletters (with your consent).", "Improve our website, products, and services.", "Prevent fraudulent transactions and protect against unauthorized activity."] },
    { icon: Cookie, title: "Cookies", content: "We use cookies and similar tracking technologies to enhance your browsing experience, analyze site traffic, and personalize content. You can control cookie preferences through your browser settings. Disabling cookies may affect some website functionality." },
    { icon: Share2, title: "Data Sharing", content: "We do not sell your personal information. We may share your data with trusted third-party service providers who assist us in operating our website, processing payments, and delivering orders. These providers are obligated to protect your information and use it only for the purposes we specify." },
    { icon: Lock, title: "Data Security", content: "We implement industry-standard security measures to protect your personal information, including SSL encryption for all data transmission. However, no method of internet transmission is 100% secure, and we cannot guarantee absolute security." },
    { icon: User, title: "Your Rights", content: "Depending on your location, you may have the right to access, correct, delete, or port your personal data. You may also opt out of marketing communications at any time by clicking the unsubscribe link in our emails or contacting us directly." },
    { icon: Shield, title: "Contact Us", content: "If you have questions about this Privacy Policy or our data practices, please contact us through our contact page." },
  ];

  return (
    <div className="mx-auto max-w-3xl px-4 py-12 sm:px-6 sm:py-16 lg:px-8">
      <h1 className="text-3xl font-semibold tracking-tight text-foreground sm:text-4xl">Privacy Policy</h1>
      <div className="mt-3">
        <span className="inline-flex rounded-full bg-muted/30 px-3 py-1 text-xs text-muted-foreground">
          Last updated: {new Date().toLocaleDateString("en-US", { month: "long", year: "numeric" })}
        </span>
      </div>

      <div className="mt-10 space-y-6">
        {sections.map((section, idx) => (
          <motion.section
            key={section.title + idx}
            initial={{ opacity: 0, y: 20 }}
            whileInView={{ opacity: 1, y: 0 }}
            viewport={{ once: true }}
            transition={{ delay: idx * 0.1 }}
            className="rounded-2xl bg-muted/20 p-6 sm:p-8"
          >
            <div className="mb-4 flex items-center gap-3">
              <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-muted/50">
                <section.icon className="h-5 w-5 text-muted-foreground" />
              </div>
              <h2 className="text-lg font-semibold tracking-tight text-foreground">{section.title}</h2>
            </div>
            {section.content && <p className="text-sm leading-relaxed text-muted-foreground">{section.content}</p>}
            {section.items && (
              <div className={"space-y-2 " + (section.content ? "mt-3" : "")}>
                {section.items.map((item, i) => (
                  <div key={i} className="flex items-start gap-3 text-sm leading-relaxed text-muted-foreground">
                    <span className="mt-1 h-1.5 w-1.5 flex-shrink-0 rounded-full bg-muted-foreground/40" />
                    <span>{item}</span>
                  </div>
                ))}
              </div>
            )}
          </motion.section>
        ))}
      </div>
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_TERMS_POLICY = `"use client";

import { FileText, DollarSign, CreditCard, Copyright, UserCheck, Scale } from "lucide-react";
import { motion } from "framer-motion";
import { useStoreConfig } from "@/lib/wemob-store";

export default function TermsOfServicePage() {
  const { config } = useStoreConfig();
  const storeName = config?.name || "Our Store";

  const sections = [
    { icon: FileText, title: "Acceptance of Terms", content: "By accessing and using the " + storeName + " website, you agree to be bound by these Terms of Service and all applicable laws and regulations. If you do not agree with any of these terms, you are prohibited from using this site." },
    { icon: DollarSign, title: "Products & Pricing", content: "All product descriptions, images, and prices are subject to change without notice. We reserve the right to modify or discontinue any product at any time. Prices are displayed in the store\u2019s configured currency and include applicable taxes unless otherwise stated. We make every effort to ensure pricing accuracy, but errors may occur. In the event of a pricing error, we reserve the right to cancel or refuse orders placed at the incorrect price." },
    { icon: CreditCard, title: "Orders & Payment", content: "By placing an order, you represent that the information provided is accurate and that you are authorized to use the payment method. We reserve the right to refuse or cancel any order for reasons including product availability, errors in pricing or product information, or suspected fraudulent activity." },
    { icon: Copyright, title: "Intellectual Property", content: "All content on this website, including text, graphics, logos, images, and software, is the property of " + storeName + " or its content suppliers and is protected by intellectual property laws. You may not reproduce, distribute, modify, or create derivative works from any content without our express written permission." },
    { icon: UserCheck, title: "User Accounts", content: "If you create an account, you are responsible for maintaining the confidentiality of your account credentials and for all activities under your account. You agree to notify us immediately of any unauthorized use of your account. We reserve the right to terminate accounts that violate these terms." },
    { icon: Scale, title: "Limitation of Liability", content: storeName + " shall not be liable for any indirect, incidental, special, consequential, or punitive damages resulting from your use of or inability to use the website or products. Our total liability for any claim arising from these terms shall not exceed the amount you paid for the product giving rise to the claim." },
    { icon: FileText, title: "Governing Law", content: "These Terms of Service shall be governed by and construed in accordance with applicable local laws, without regard to conflict of law principles." },
    { icon: FileText, title: "Changes to Terms", content: "We reserve the right to update these Terms of Service at any time. Changes will be effective immediately upon posting. Your continued use of the website after changes constitutes acceptance of the updated terms." },
  ];

  return (
    <div className="mx-auto max-w-3xl px-4 py-12 sm:px-6 sm:py-16 lg:px-8">
      <h1 className="text-3xl font-semibold tracking-tight text-foreground sm:text-4xl">Terms of Service</h1>
      <div className="mt-3">
        <span className="inline-flex rounded-full bg-muted/30 px-3 py-1 text-xs text-muted-foreground">
          Last updated: {new Date().toLocaleDateString("en-US", { month: "long", year: "numeric" })}
        </span>
      </div>

      <div className="mt-10 space-y-6">
        {sections.map((section, idx) => (
          <motion.section
            key={section.title}
            initial={{ opacity: 0, y: 20 }}
            whileInView={{ opacity: 1, y: 0 }}
            viewport={{ once: true }}
            transition={{ delay: idx * 0.1 }}
            className="rounded-2xl bg-muted/20 p-6 sm:p-8"
          >
            <div className="mb-4 flex items-center gap-3">
              <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-muted/50">
                <section.icon className="h-5 w-5 text-muted-foreground" />
              </div>
              <h2 className="text-lg font-semibold tracking-tight text-foreground">{section.title}</h2>
            </div>
            <p className="text-sm leading-relaxed text-muted-foreground">{section.content}</p>
          </motion.section>
        ))}
      </div>
    </div>
  );
}
`
