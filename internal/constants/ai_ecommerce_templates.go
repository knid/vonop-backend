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
              Out of Stock
            </div>
          )}
          {onSale && !outOfStock && (
            <div className="absolute left-2.5 top-2.5 rounded-full bg-rose-500 px-2.5 py-1 text-[11px] font-medium uppercase tracking-[0.15em] text-white">
              Sale
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
                <p className="mb-3 text-center text-[11px] text-muted-foreground">
                  Taxes and shipping calculated at checkout
                </p>
                <div className="flex flex-col gap-3">
                  <Link
                    href="/checkout"
                    onClick={onClose}
                    className="flex w-full items-center justify-center gap-2 rounded-full bg-foreground px-4 py-3 text-sm font-semibold text-background transition-opacity hover:opacity-90"
                  >
                    Secure Checkout
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
              className="absolute left-3 top-1/2 flex h-10 w-10 -translate-y-1/2 items-center justify-center rounded-full bg-background/90 shadow-sm opacity-0 backdrop-blur-sm transition-all group-hover:opacity-100 hover:bg-background"
              aria-label="Previous image"
            >
              <ChevronLeft className="h-4 w-4" />
            </button>
            <button
              onClick={handleNext}
              className="absolute right-3 top-1/2 flex h-10 w-10 -translate-y-1/2 items-center justify-center rounded-full bg-background/90 shadow-sm opacity-0 backdrop-blur-sm transition-all group-hover:opacity-100 hover:bg-background"
              aria-label="Next image"
            >
              <ChevronRight className="h-4 w-4" />
            </button>
            <div className="absolute bottom-3 left-3 rounded-full bg-background/80 px-2.5 py-1 text-[11px] font-medium tabular-nums text-foreground/80 backdrop-blur-sm opacity-0 transition-opacity group-hover:opacity-100">
              {(selectedIndex + 1) + " / " + images.length}
            </div>
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
import { Loader2, AlertTriangle, Check, Mail, MapPin, CreditCard, ShieldCheck } from "lucide-react";
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

  const inputClass = "w-full rounded-xl border border-border/50 bg-transparent px-4 py-3 text-sm outline-none transition-all focus:border-foreground focus:ring-1 focus:ring-foreground/20";

  return (
    <div className="grid gap-8 lg:grid-cols-[1fr_400px]">
      <form onSubmit={handleSubmit} className="space-y-6">
        {/* Express Checkout */}
        <div className="rounded-2xl border border-border/40 p-5">
          <p className="mb-3 text-center text-xs font-medium uppercase tracking-[0.15em] text-muted-foreground">Express Checkout</p>
          <div className="flex gap-3">
            <button type="button" className="flex h-12 flex-1 items-center justify-center rounded-xl bg-black text-white text-sm font-medium transition-opacity hover:opacity-90 dark:bg-white dark:text-black">
              Apple Pay
            </button>
            <button type="button" className="flex h-12 flex-1 items-center justify-center rounded-xl bg-[#ffc439] text-[#003087] text-sm font-semibold transition-opacity hover:opacity-90">
              PayPal
            </button>
          </div>
          <div className="relative my-5">
            <div className="absolute inset-0 flex items-center"><div className="w-full border-t border-border/40" /></div>
            <div className="relative flex justify-center"><span className="bg-background px-3 text-xs text-muted-foreground">or continue with</span></div>
          </div>
        </div>

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
          <div className="mb-5 flex items-center gap-2">
            <Mail className="h-4 w-4 text-muted-foreground" />
            <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Contact Information</h2>
          </div>
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
          <div className="mb-5 flex items-center gap-2">
            <MapPin className="h-4 w-4 text-muted-foreground" />
            <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Shipping Address</h2>
          </div>
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
            <div className="mb-5 flex items-center gap-2">
              <CreditCard className="h-4 w-4 text-muted-foreground" />
              <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Payment Method</h2>
            </div>
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

        <div className="flex items-center justify-center gap-1.5 text-xs text-muted-foreground/60">
          <ShieldCheck className="h-3.5 w-3.5" />
          <span>Your information is encrypted and secure</span>
        </div>
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
        <motion.div
          animate={{ scale: [1, 1.04, 1] }}
          transition={{ duration: 3, repeat: Infinity, ease: "easeInOut" }}
          className="mb-5 flex h-20 w-20 items-center justify-center rounded-full bg-muted/50 text-muted-foreground/40"
        >
          {icon}
        </motion.div>
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
      <div className="flex flex-col gap-6 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex-1">
          <h3 className="text-xl font-semibold tracking-tight">Customer Reviews</h3>
          {reviewCount > 0 && (
            <div className="mt-3 flex flex-col gap-4 sm:flex-row sm:items-center sm:gap-8">
              <div className="flex items-center gap-3">
                <span className="text-4xl font-bold tracking-tight text-foreground">{averageRating.toFixed(1)}</span>
                <div>
                  <StarRating rating={Math.round(averageRating)} size="md" />
                  <p className="mt-0.5 text-xs text-muted-foreground">{reviewCount} review{reviewCount !== 1 ? "s" : ""}</p>
                </div>
              </div>
              {/* Rating Distribution */}
              <div className="flex-1 space-y-1.5">
                {[5, 4, 3, 2, 1].map((star) => {
                  const count = reviews.filter((r) => r.rating === star).length;
                  const pct = reviewCount > 0 ? (count / reviewCount) * 100 : 0;
                  return (
                    <div key={star} className="flex items-center gap-2 text-xs">
                      <span className="w-3 text-right text-muted-foreground">{star}</span>
                      <Star className="h-3 w-3 fill-foreground text-foreground" />
                      <div className="h-2 flex-1 overflow-hidden rounded-full bg-muted/50">
                        <div className="h-full rounded-full bg-foreground transition-all duration-500" style={{ width: pct + "%" }} />
                      </div>
                      <span className="w-8 text-right text-muted-foreground">{count}</span>
                    </div>
                  );
                })}
              </div>
            </div>
          )}
        </div>
        {!showForm && !submitted && (
          <button onClick={() => setShowForm(true)} className="flex-shrink-0 rounded-full border border-foreground px-5 py-2.5 text-sm font-medium text-foreground transition-colors hover:bg-foreground hover:text-background">
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
              <div className="mb-3 flex items-start justify-between">
                <div className="flex items-center gap-3">
                  <div className="flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-full bg-muted text-sm font-semibold text-muted-foreground">
                    {review.customer_name.charAt(0).toUpperCase()}
                  </div>
                  <div>
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium text-foreground">{review.customer_name}</span>
                      {review.is_verified && (
                        <span className="rounded-full bg-green-100 px-2 py-0.5 text-[10px] font-medium text-green-700 dark:bg-green-500/10 dark:text-green-400">Verified</span>
                      )}
                    </div>
                    <StarRating rating={review.rating} />
                  </div>
                </div>
                <span className="text-xs text-muted-foreground">
                  {new Date(review.created_at).toLocaleDateString()}
                </span>
              </div>
              {review.title && <h4 className="mb-1 font-medium">{review.title}</h4>}
              {review.body && <p className="text-sm leading-relaxed text-muted-foreground">{review.body}</p>}
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
      className={"flex items-center justify-center rounded-full transition-all bg-background/80 backdrop-blur-sm " +
        (active ? "text-red-500 shadow-md shadow-red-500/20" : "shadow-sm text-muted-foreground hover:text-red-500") +
        " " + btnSize + " " + className}
      aria-label={active ? "Remove from wishlist" : "Add to wishlist"}
    >
      <Heart className={iconSize + (active ? " fill-current" : "")} />
    </motion.button>
  );
}
`

const ECOMMERCE_TEMPLATE_SITE_HEADER = `"use client";

import React, { useState, useEffect, useRef } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { ShoppingBag, Search, Menu, X, ChevronDown, User } from "lucide-react";
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
  const [shopOpen, setShopOpen] = useState(false);
  const [mobileShopOpen, setMobileShopOpen] = useState(false);
  const [searchOpen, setSearchOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [announcementVisible, setAnnouncementVisible] = useState(true);
  const shopTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const searchInputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const handleScroll = () => setScrolled(window.scrollY > 10);
    window.addEventListener("scroll", handleScroll, { passive: true });
    return () => window.removeEventListener("scroll", handleScroll);
  }, []);

  useEffect(() => {
    setMobileMenuOpen(false);
    setMobileShopOpen(false);
    setSearchOpen(false);
  }, [pathname]);

  useEffect(() => {
    if (searchOpen && searchInputRef.current) searchInputRef.current.focus();
  }, [searchOpen]);

  const navLinks = [
    { label: "Home", href: "/" },
    { label: "About", href: "/about" },
    { label: "Contact", href: "/contact" },
  ];

  const shopLinks = [
    { label: "All Products", href: "/products" },
    ...(categories ?? []).map((cat) => ({
      label: cat.name,
      href: "/categories/" + cat.slug,
    })),
  ];

  const isActive = (href: string) =>
    href === "/" ? pathname === "/" : pathname.startsWith(href);

  const isShopActive = pathname.startsWith("/products") || pathname.startsWith("/categories");

  const handleShopEnter = () => {
    if (shopTimeoutRef.current) clearTimeout(shopTimeoutRef.current);
    setShopOpen(true);
  };

  const handleShopLeave = () => {
    shopTimeoutRef.current = setTimeout(() => setShopOpen(false), 150);
  };

  return (
    <>
      {/* Announcement Bar */}
      <AnimatePresence>
        {announcementVisible && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: "auto", opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.2 }}
            className="relative z-50 overflow-hidden bg-foreground text-background"
          >
            <div className="flex items-center justify-center px-4 py-2 text-center text-xs font-medium tracking-wide sm:text-sm">
              <span>Free shipping on orders over $50</span>
              <span className="mx-2 hidden text-background/40 sm:inline">|</span>
              <Link href="/products" className="hidden underline underline-offset-2 transition-opacity hover:opacity-80 sm:inline">
                Shop Now
              </Link>
              <button
                onClick={() => setAnnouncementVisible(false)}
                className="absolute right-3 top-1/2 -translate-y-1/2 rounded-full p-0.5 transition-opacity hover:opacity-70"
                aria-label="Dismiss announcement"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            </div>
          </motion.div>
        )}
      </AnimatePresence>

      <header
        className={
          "sticky top-0 z-50 w-full transition-all duration-300 " +
          (scrolled
            ? "bg-background/80 backdrop-blur-xl border-b border-border/30 shadow-sm"
            : "bg-background border-b border-transparent")
        }
      >
        <div className={"mx-auto flex max-w-7xl items-center justify-between px-4 transition-all duration-300 sm:px-6 lg:px-8 " + (scrolled ? "h-14" : "h-16")}>
          <Link href="/" className="text-xl font-bold tracking-tight text-foreground">
            {storeConfig?.name}
          </Link>

          <nav className="hidden items-center gap-1 md:flex">
            <Link
              href="/"
              className={"relative px-3 py-2 text-sm font-medium transition-colors " +
                (pathname === "/" ? "text-foreground" : "text-muted-foreground hover:text-foreground")}
            >
              Home
              {pathname === "/" && (
                <motion.div
                  layoutId="nav-underline"
                  className="absolute -bottom-px left-0 right-0 h-0.5 bg-foreground"
                  transition={{ type: "spring", bounce: 0.2, duration: 0.5 }}
                />
              )}
            </Link>

            {/* Shop dropdown */}
            <div
              className="relative"
              onMouseEnter={handleShopEnter}
              onMouseLeave={handleShopLeave}
            >
              <Link
                href="/products"
                className={"relative inline-flex items-center gap-1 px-3 py-2 text-sm font-medium transition-colors " +
                  (isShopActive ? "text-foreground" : "text-muted-foreground hover:text-foreground")}
              >
                Shop
                <ChevronDown className={"h-3.5 w-3.5 transition-transform " + (shopOpen ? "rotate-180" : "")} />
                {isShopActive && (
                  <motion.div
                    layoutId="nav-underline"
                    className="absolute -bottom-px left-0 right-0 h-0.5 bg-foreground"
                    transition={{ type: "spring", bounce: 0.2, duration: 0.5 }}
                  />
                )}
              </Link>

              <AnimatePresence>
                {shopOpen && (
                  <motion.div
                    initial={{ opacity: 0, y: 4 }}
                    animate={{ opacity: 1, y: 0 }}
                    exit={{ opacity: 0, y: 4 }}
                    transition={{ duration: 0.15 }}
                    className="absolute left-0 top-full pt-2"
                  >
                    <div className="w-52 overflow-hidden rounded-xl border border-border/40 bg-background shadow-lg">
                      <div className="py-1.5">
                        {shopLinks.map((link, i) => (
                          <Link
                            key={link.href + "-" + i}
                            href={link.href}
                            className={"block px-4 py-2 text-sm transition-colors " +
                              (isActive(link.href)
                                ? "bg-muted text-foreground font-medium"
                                : "text-muted-foreground hover:bg-muted/50 hover:text-foreground")}
                          >
                            {link.label}
                          </Link>
                        ))}
                      </div>
                    </div>
                  </motion.div>
                )}
              </AnimatePresence>
            </div>

            {navLinks.slice(1).map((link) => (
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

          <div className="flex items-center gap-1">
            {/* Inline Search */}
            <div className="relative hidden md:block">
              <AnimatePresence>
                {searchOpen && (
                  <motion.div
                    initial={{ width: 0, opacity: 0 }}
                    animate={{ width: 220, opacity: 1 }}
                    exit={{ width: 0, opacity: 0 }}
                    transition={{ duration: 0.2 }}
                    className="absolute right-0 top-1/2 -translate-y-1/2 overflow-hidden"
                  >
                    <form action="/products" method="get" className="flex items-center">
                      <input
                        ref={searchInputRef}
                        name="q"
                        type="text"
                        value={searchQuery}
                        onChange={(e) => setSearchQuery(e.target.value)}
                        placeholder="Search..."
                        className="h-9 w-full rounded-full border border-border/60 bg-muted/30 pl-4 pr-9 text-sm text-foreground placeholder:text-muted-foreground focus:border-foreground focus:outline-none"
                      />
                      <button type="button" onClick={() => { setSearchOpen(false); setSearchQuery(""); }} className="absolute right-2 top-1/2 -translate-y-1/2 p-0.5 text-muted-foreground hover:text-foreground">
                        <X className="h-3.5 w-3.5" />
                      </button>
                    </form>
                  </motion.div>
                )}
              </AnimatePresence>
              {!searchOpen && (
                <button
                  onClick={() => setSearchOpen(true)}
                  className="flex h-10 w-10 items-center justify-center rounded-full text-muted-foreground transition-colors hover:text-foreground"
                  aria-label="Search products"
                >
                  <Search className="h-5 w-5" />
                </button>
              )}
            </div>
            <Link
              href="/products?q="
              className="flex h-10 w-10 items-center justify-center rounded-full text-muted-foreground transition-colors hover:text-foreground md:hidden"
              aria-label="Search products"
            >
              <Search className="h-5 w-5" />
            </Link>

            <Link
              href="/account"
              className="hidden h-10 w-10 items-center justify-center rounded-full text-muted-foreground transition-colors hover:text-foreground sm:flex"
              aria-label="Account"
            >
              <User className="h-5 w-5" />
            </Link>

            <button
              onClick={() => setCartOpen(true)}
              className="relative flex h-10 w-10 items-center justify-center rounded-full text-muted-foreground transition-colors hover:text-foreground"
              aria-label="Open cart"
            >
              <ShoppingBag className="h-5 w-5" />
              <AnimatePresence>
                {itemCount > 0 && (
                  <motion.span
                    key={itemCount}
                    initial={{ scale: 0 }}
                    animate={{ scale: 1 }}
                    exit={{ scale: 0 }}
                    className="absolute -right-0.5 -top-0.5 flex h-4.5 w-4.5 items-center justify-center rounded-full bg-foreground text-[10px] font-semibold text-background"
                  >
                    {itemCount > 99 ? "99+" : itemCount}
                  </motion.span>
                )}
              </AnimatePresence>
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
              <motion.div initial={{ opacity: 0, y: 20 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0 }}>
                <Link
                  href="/"
                  className={"block px-4 py-3 text-center text-2xl font-semibold tracking-tight transition-colors " +
                    (pathname === "/" ? "text-foreground" : "text-muted-foreground hover:text-foreground")}
                >
                  Home
                </Link>
              </motion.div>

              <motion.div initial={{ opacity: 0, y: 20 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0.05 }}>
                <button
                  onClick={() => setMobileShopOpen(!mobileShopOpen)}
                  className={"flex items-center gap-2 px-4 py-3 text-2xl font-semibold tracking-tight transition-colors " +
                    (isShopActive ? "text-foreground" : "text-muted-foreground hover:text-foreground")}
                >
                  Shop
                  <ChevronDown className={"h-5 w-5 transition-transform " + (mobileShopOpen ? "rotate-180" : "")} />
                </button>
                <AnimatePresence>
                  {mobileShopOpen && (
                    <motion.div
                      initial={{ height: 0, opacity: 0 }}
                      animate={{ height: "auto", opacity: 1 }}
                      exit={{ height: 0, opacity: 0 }}
                      transition={{ duration: 0.2 }}
                      className="overflow-hidden"
                    >
                      <div className="flex flex-col items-center gap-1 pb-2">
                        {shopLinks.map((link, i) => (
                          <Link
                            key={link.href + "-" + i}
                            href={link.href}
                            className={"block px-4 py-1.5 text-center text-lg transition-colors " +
                              (isActive(link.href) ? "text-foreground font-medium" : "text-muted-foreground hover:text-foreground")}
                          >
                            {link.label}
                          </Link>
                        ))}
                      </div>
                    </motion.div>
                  )}
                </AnimatePresence>
              </motion.div>

              {navLinks.slice(1).map((link, i) => (
                <motion.div
                  key={link.href}
                  initial={{ opacity: 0, y: 20 }}
                  animate={{ opacity: 1, y: 0 }}
                  transition={{ delay: (i + 2) * 0.05 }}
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

import React, { useState } from "react";
import Link from "next/link";
import { ArrowUp } from "lucide-react";
import { useCategories, useStoreConfig } from "@/lib/wemob-store";

export function SiteFooter() {
  const { categories } = useCategories();
  const { config: storeConfig } = useStoreConfig();
  const [email, setEmail] = useState("");
  const [subscribed, setSubscribed] = useState(false);

  const allCategories = categories ?? [];
  const shopLinks = [
    { label: "All Products", href: "/products" },
    ...allCategories.slice(0, 6).map((cat) => ({
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
              <a
                href="#"
                className="flex h-9 w-9 items-center justify-center rounded-full border border-border/40 text-muted-foreground transition-colors hover:border-foreground hover:text-foreground"
                aria-label="Instagram"
              >
                <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <rect x="2" y="2" width="20" height="20" rx="5" />
                  <circle cx="12" cy="12" r="5" />
                  <circle cx="17.5" cy="6.5" r="1.5" fill="currentColor" stroke="none" />
                </svg>
              </a>
              <a
                href="#"
                className="flex h-9 w-9 items-center justify-center rounded-full border border-border/40 text-muted-foreground transition-colors hover:border-foreground hover:text-foreground"
                aria-label="Twitter"
              >
                <svg className="h-4 w-4" viewBox="0 0 24 24" fill="currentColor">
                  <path d="M18.244 2.25h3.308l-7.227 8.26 8.502 11.24H16.17l-5.214-6.817L4.99 21.75H1.68l7.73-8.835L1.254 2.25H8.08l4.713 6.231zm-1.161 17.52h1.833L7.084 4.126H5.117z" />
                </svg>
              </a>
              <a
                href="#"
                className="flex h-9 w-9 items-center justify-center rounded-full border border-border/40 text-muted-foreground transition-colors hover:border-foreground hover:text-foreground"
                aria-label="Facebook"
              >
                <svg className="h-4 w-4" viewBox="0 0 24 24" fill="currentColor">
                  <path d="M9.101 23.691v-7.98H6.627v-3.667h2.474v-1.58c0-4.085 1.848-5.978 5.858-5.978.401 0 1.09.044 1.613.115v3.146c-.427-.044-.72-.065-.95-.065-1.35 0-1.872.513-1.872 1.846v2.516h3.332l-.468 3.667h-2.864v8.245C19.396 23.238 24 18.179 24 12.044c0-6.627-5.373-12-12-12s-12 5.373-12 12c0 5.628 3.874 10.35 9.101 11.647z" />
                </svg>
              </a>
            </div>

            {/* Newsletter */}
            <div className="mt-8">
              <h3 className="text-[11px] font-medium uppercase tracking-[0.2em] text-muted-foreground/50">Newsletter</h3>
              <p className="mt-3 text-xs text-muted-foreground">Get the latest updates, exclusive offers, and new arrivals delivered to your inbox.</p>
              {subscribed ? (
                <p className="mt-3 text-xs font-medium text-green-600 dark:text-green-400">Thank you for subscribing!</p>
              ) : (
                <form onSubmit={(e) => { e.preventDefault(); if (email.trim()) setSubscribed(true); }} className="mt-3 flex gap-2">
                  <input
                    type="email"
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    placeholder="your@email.com"
                    required
                    className="h-9 flex-1 rounded-full border border-border/60 bg-background px-4 text-xs text-foreground placeholder:text-muted-foreground focus:border-foreground focus:outline-none"
                  />
                  <button
                    type="submit"
                    className="h-9 rounded-full bg-foreground px-4 text-xs font-medium text-background transition-opacity hover:opacity-90"
                  >
                    Subscribe
                  </button>
                </form>
              )}
            </div>
          </div>

          <div>
            <h3 className="text-[11px] font-medium uppercase tracking-[0.2em] text-muted-foreground/50">
              Shop
            </h3>
            <ul className="mt-4 space-y-2.5">
              {shopLinks.map((link, i) => (
                <li key={link.href + "-" + i}>
                  <Link
                    href={link.href}
                    className="text-sm text-muted-foreground transition-colors hover:text-foreground"
                  >
                    {link.label}
                  </Link>
                </li>
              ))}
              {allCategories.length > 6 && (
                <li>
                  <Link
                    href="/products"
                    className="text-sm font-medium text-muted-foreground transition-colors hover:text-foreground"
                  >
                    {"View All \u2192"}
                  </Link>
                </li>
              )}
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
          <div className="flex flex-col items-center gap-6 sm:flex-row sm:justify-between">
            <p className="text-xs text-muted-foreground/60">
              &copy; {new Date().getFullYear()} {storeConfig?.name}. All rights reserved.
            </p>

            {/* Payment method icons */}
            <div className="flex items-center gap-3">
              <span className="mr-1 text-[10px] uppercase tracking-wider text-muted-foreground/40">We accept</span>
              <svg className="h-6 w-auto text-muted-foreground/50" viewBox="0 0 48 32" fill="none"><rect width="48" height="32" rx="4" fill="currentColor" opacity="0.1"/><path d="M19.5 20.5l2-9h3l-2 9h-3zm13-9l-3 6.2-.4-2-1-5.4s-.1-.8-1.2-.8h-4.7l-.1.3s1.4.3 3 1.3l2.3 8.4h3.1l4.7-8h-2.7zm-19 0l-3 9h-3.2l-1.8-7.2c-.1-.4-.2-.6-.5-.7-.6-.3-1.5-.6-2.3-.8l.1-.3h4.8c.6 0 1.2.4 1.3 1.2l1.2 6.2 2.9-7.4h3.5zm26.2 6.2c0-2.5-3.4-2.6-3.4-3.7 0-.3.3-.7 1.1-.8.4 0 1.5-.1 2.7.5l.5-2.2c-.7-.2-1.5-.5-2.6-.5-2.7 0-4.7 1.5-4.7 3.6 0 1.6 1.4 2.4 2.4 3 1.1.5 1.4.8 1.4 1.3 0 .7-.8 1-1.6 1-.7 0-1.9-.3-2.5-.5l-.5 2.2c.6.3 1.6.5 2.7.5 2.9 0 4.8-1.4 4.8-3.7l-.3.3z" fill="currentColor" opacity="0.5"/></svg>
              <svg className="h-6 w-auto text-muted-foreground/50" viewBox="0 0 48 32" fill="none"><rect width="48" height="32" rx="4" fill="currentColor" opacity="0.1"/><circle cx="19" cy="16" r="8" fill="currentColor" opacity="0.2"/><circle cx="29" cy="16" r="8" fill="currentColor" opacity="0.3"/></svg>
              <svg className="h-6 w-auto text-muted-foreground/50" viewBox="0 0 48 32" fill="none"><rect width="48" height="32" rx="4" fill="currentColor" opacity="0.1"/><text x="24" y="20" textAnchor="middle" fontSize="10" fontWeight="bold" fill="currentColor" opacity="0.4">AMEX</text></svg>
              <svg className="h-6 w-auto text-muted-foreground/50" viewBox="0 0 48 32" fill="none"><rect width="48" height="32" rx="4" fill="currentColor" opacity="0.1"/><text x="24" y="19" textAnchor="middle" fontSize="8" fontWeight="600" fontStyle="italic" fill="currentColor" opacity="0.4">PayPal</text></svg>
            </div>
          </div>

          <p className="mt-6 text-center text-[11px] text-muted-foreground/40">
            Secure shopping guaranteed. All transactions are encrypted and your data is protected.
          </p>

          {/* Back to top */}
          <div className="mt-4 flex justify-center">
            <button
              onClick={() => window.scrollTo({ top: 0, behavior: "smooth" })}
              className="flex items-center gap-1.5 text-xs text-muted-foreground/50 transition-colors hover:text-muted-foreground"
            >
              <ArrowUp className="h-3.5 w-3.5" />
              Back to top
            </button>
          </div>
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
    <section className="relative overflow-hidden py-20 sm:py-28">
      <div className="absolute inset-0 bg-gradient-to-b from-muted/40 via-muted/20 to-transparent" />
      <div className="relative mx-auto max-w-2xl px-4 text-center sm:px-6 lg:px-8">
        <motion.div
          initial={{ opacity: 0, y: 20 }}
          whileInView={{ opacity: 1, y: 0 }}
          viewport={{ once: true }}
          transition={{ duration: 0.5 }}
        >
          <p className="mb-4 text-xs font-medium uppercase tracking-[0.2em] text-muted-foreground">
            Join 10,000+ happy customers
          </p>
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
              disabled={discountLoading || !discountCode.trim() || discountSuccess}
              className={"h-11 rounded-full px-5 text-sm font-medium transition-colors disabled:opacity-50 " + (discountSuccess ? "bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400" : "bg-muted text-foreground hover:bg-muted/80")}
            >
              {discountSuccess ? (
                <span className="flex items-center gap-1"><Check className="h-3.5 w-3.5" /> Applied</span>
              ) : discountLoading ? "Applying..." : "Apply"}
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

        <div className="mt-8 lg:mt-0 lg:sticky lg:top-24 lg:self-start">
          <OrderSummary cart={cart} />
          <Link
            href="/checkout"
            className="mt-4 flex h-14 w-full items-center justify-center gap-2 rounded-full bg-foreground text-sm font-semibold text-background transition-opacity hover:opacity-90"
          >
            <Lock className="h-4 w-4" />
            Secure Checkout
          </Link>
          <div className="mt-4 rounded-xl border border-border/40 p-4 text-center">
            <div className="flex items-center justify-center gap-4 text-xs text-muted-foreground">
              <div className="flex items-center gap-1">
                <Truck className="h-3.5 w-3.5" />
                <span>Free shipping over $50</span>
              </div>
            </div>
            <p className="mt-2 text-[11px] text-muted-foreground/50">
              {"Estimated delivery: " + new Date(Date.now() + 5 * 24 * 60 * 60 * 1000).toLocaleDateString("en-US", { month: "short", day: "numeric" }) + " \u2013 " + new Date(Date.now() + 7 * 24 * 60 * 60 * 1000).toLocaleDateString("en-US", { month: "short", day: "numeric" })}
            </p>
          </div>
        </div>
      </div>

      {/* You Might Also Like */}
      {suggestions.length > 0 && (
        <div className="mt-16 border-t border-border/40 pt-12">
          <h2 className="text-xl font-semibold tracking-tight text-foreground">You Might Also Like</h2>
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
`

const ECOMMERCE_TEMPLATE_CHECKOUT_PAGE = `"use client";

import React from "react";
import Link from "next/link";
import { ArrowLeft, Check, Lock, Shield } from "lucide-react";
import { motion } from "framer-motion";
import { CheckoutForm } from "@/components/ecommerce/checkout-form";

export default function CheckoutPage() {
  var steps = ["Cart", "Information", "Payment"];

  return (
    <div className="mx-auto max-w-4xl px-4 py-12 sm:px-6 sm:py-16 lg:px-8">
      {/* Security trust bar */}
      <div className="mb-8 flex items-center justify-center gap-2 rounded-xl bg-muted/20 px-4 py-3 text-center text-xs text-muted-foreground">
        <Lock className="h-3.5 w-3.5 flex-shrink-0" />
        <span>Secure Checkout \u2014 Your information is encrypted and protected</span>
      </div>

      {/* Step progress with connecting lines and checkmarks */}
      <div className="mb-10">
        <div className="flex items-center justify-center">
          {steps.map(function(step, i) {
            var isCompleted = i < 1;
            var isCurrent = i === 1;
            return (
              <React.Fragment key={step}>
                {i > 0 && (
                  <div className={"mx-1 h-0.5 w-10 rounded-full sm:mx-2 sm:w-16 " + (isCompleted ? "bg-foreground" : "bg-border/40")} />
                )}
                <div className="flex items-center gap-2">
                  <div className={"flex h-8 w-8 items-center justify-center rounded-full text-xs font-semibold transition-colors " +
                    (isCompleted ? "bg-foreground text-background" : isCurrent ? "bg-foreground text-background ring-4 ring-foreground/10" : "border-2 border-border/40 text-muted-foreground/50")}>
                    {isCompleted ? <Check className="h-4 w-4" /> : i + 1}
                  </div>
                  <span className={"hidden text-sm sm:block " + (isCurrent ? "font-semibold text-foreground" : isCompleted ? "font-medium text-foreground" : "text-muted-foreground/50")}>
                    {step}
                  </span>
                </div>
              </React.Fragment>
            );
          })}
        </div>
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

      {/* Bottom trust indicators */}
      <div className="mt-8 flex flex-wrap items-center justify-center gap-6 text-xs text-muted-foreground/50">
        <div className="flex items-center gap-1.5">
          <Shield className="h-3.5 w-3.5" />
          <span>SSL Encrypted</span>
        </div>
        <div className="flex items-center gap-1.5">
          <Lock className="h-3.5 w-3.5" />
          <span>Secure Payment</span>
        </div>
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

      {/* What Happens Next */}
      <div className="mt-10 rounded-xl bg-muted/20 p-6 text-left">
        <h3 className="mb-4 text-sm font-medium uppercase tracking-[0.15em] text-muted-foreground">What Happens Next</h3>
        <div className="space-y-4">
          {[
            { step: "1", title: "Order Confirmed", desc: "We have received your order and are preparing it." },
            { step: "2", title: "Processing", desc: "Your items are being packed with care." },
            { step: "3", title: "Shipped", desc: "You will receive a tracking number via email." },
            { step: "4", title: "Delivered", desc: "Your order arrives at your doorstep." },
          ].map((item, i) => (
            <div key={item.step} className="flex items-start gap-3">
              <div className={"flex h-7 w-7 flex-shrink-0 items-center justify-center rounded-full text-xs font-semibold " + (i === 0 ? "bg-green-100 text-green-700 dark:bg-green-500/10 dark:text-green-400" : "bg-muted text-muted-foreground")}>
                {i === 0 ? <CheckCircle className="h-4 w-4" /> : item.step}
              </div>
              <div>
                <p className={"text-sm font-medium " + (i === 0 ? "text-foreground" : "text-muted-foreground")}>{item.title}</p>
                <p className="text-xs text-muted-foreground/70">{item.desc}</p>
              </div>
            </div>
          ))}
        </div>
      </div>

      <div className="mt-8 flex flex-col items-center gap-3">
        <Link
          href="/products"
          className="inline-flex h-11 items-center rounded-full bg-foreground px-6 text-sm font-medium text-background transition-opacity hover:opacity-90"
        >
          Continue Shopping
        </Link>
        <Link href="/contact" className="text-sm text-muted-foreground underline underline-offset-4 transition-colors hover:text-foreground">
          Need help with your order?
        </Link>
      </div>
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_CHECKOUT_CANCEL_PAGE = `"use client";

import React, { useState } from "react";
import Link from "next/link";
import { XCircle, ChevronDown, MessageCircle } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";

export default function CheckoutCancelPage() {
  var faqs = [
    { q: "Why was my payment cancelled?", a: "Payments can be cancelled if you close the payment window, if there are insufficient funds, or if your bank declined the transaction. No charges have been made to your account." },
    { q: "Was I charged?", a: "No. If a payment is cancelled, no charges are applied. If you see a pending charge, it will be automatically reversed within 3\u20135 business days." },
    { q: "Can I use a different payment method?", a: "Yes. When you try again, you can select a different payment method at checkout." },
    { q: "Is my cart still saved?", a: "Yes. Your cart items are saved and will be available when you return to checkout." },
  ];

  var [openFaq, setOpenFaq] = useState<number | null>(null);

  return (
    <div className="mx-auto max-w-lg px-4 py-16 sm:px-6 sm:py-20">
      <div className="text-center">
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
          Your payment was not completed. Your cart is still saved and no charges were made.
        </p>

        <div className="mt-8 flex flex-col gap-3 sm:flex-row sm:justify-center">
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

      {/* FAQ Section */}
      <div className="mt-12 border-t border-border/40 pt-10">
        <h2 className="mb-5 text-center text-sm font-medium uppercase tracking-[0.15em] text-muted-foreground">
          Common Questions
        </h2>
        <div className="space-y-2">
          {faqs.map(function(faq, i) {
            return (
              <div key={i} className="rounded-xl border border-border/40 bg-muted/10">
                <button
                  onClick={function() { setOpenFaq(openFaq === i ? null : i); }}
                  className="flex w-full items-center justify-between px-5 py-4 text-left text-sm font-medium text-foreground"
                >
                  {faq.q}
                  <ChevronDown className={"h-4 w-4 flex-shrink-0 text-muted-foreground transition-transform duration-200 " + (openFaq === i ? "rotate-180" : "")} />
                </button>
                <AnimatePresence>
                  {openFaq === i && (
                    <motion.div
                      initial={{ height: 0, opacity: 0 }}
                      animate={{ height: "auto", opacity: 1 }}
                      exit={{ height: 0, opacity: 0 }}
                      transition={{ duration: 0.2 }}
                      className="overflow-hidden"
                    >
                      <p className="px-5 pb-4 text-sm leading-relaxed text-muted-foreground">
                        {faq.a}
                      </p>
                    </motion.div>
                  )}
                </AnimatePresence>
              </div>
            );
          })}
        </div>
      </div>

      {/* Support link */}
      <div className="mt-8 text-center">
        <Link
          href="/contact"
          className="inline-flex items-center gap-1.5 text-sm text-muted-foreground underline underline-offset-4 transition-colors hover:text-foreground"
        >
          <MessageCircle className="h-3.5 w-3.5" />
          Contact Support
        </Link>
      </div>
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_PRODUCTS_PAGE = `"use client";

import React, { useState, useMemo } from "react";
import Image from "next/image";
import Link from "next/link";
import { Search, SlidersHorizontal, ChevronDown, ChevronRight, LayoutGrid, List, X, ShoppingBag } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { useProducts, useCategories, formatPrice } from "@/lib/wemob-store";
import { ProductCard } from "@/components/ecommerce/product-card";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/components/ecommerce/empty-state";

type SortOption = "featured" | "newest" | "price-asc" | "price-desc" | "name";
type ViewMode = "grid" | "list";

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
  const [sort, setSort] = useState<SortOption>("featured");
  const [showSort, setShowSort] = useState(false);
  const [viewMode, setViewMode] = useState<ViewMode>("grid");
  const [showFilters, setShowFilters] = useState(false);
  const [priceMin, setPriceMin] = useState("");
  const [priceMax, setPriceMax] = useState("");
  const [onSaleOnly, setOnSaleOnly] = useState(false);
  const [inStockOnly, setInStockOnly] = useState(false);
  const [openSections, setOpenSections] = useState<Record<string, boolean>>({ categories: true, price: true, filters: true });

  const toggleSection = (key: string) => {
    setOpenSections((prev) => ({ ...prev, [key]: !prev[key] }));
  };

  const sortLabels: Record<SortOption, string> = {
    featured: "Featured",
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

    if (priceMin) {
      const min = parseFloat(priceMin) * 100;
      result = result.filter((p) => p.price_in_cents >= min);
    }
    if (priceMax) {
      const max = parseFloat(priceMax) * 100;
      result = result.filter((p) => p.price_in_cents <= max);
    }

    if (onSaleOnly) {
      result = result.filter((p) => p.compare_at_price && p.compare_at_price > p.price_in_cents);
    }
    if (inStockOnly) {
      result = result.filter((p) => p.in_stock !== false);
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
  }, [products, categories, search, selectedCategory, sort, priceMin, priceMax, onSaleOnly, inStockOnly]);

  const activeFilterCount = [selectedCategory, priceMin, priceMax, onSaleOnly, inStockOnly].filter(Boolean).length;

  const clearFilters = () => {
    setSelectedCategory(null);
    setPriceMin("");
    setPriceMax("");
    setOnSaleOnly(false);
    setInStockOnly(false);
  };

  const filterContent = (
    <div className="space-y-8">
      <div className="relative">
        <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
        <input
          type="text"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search products..."
          className="h-10 w-full rounded-lg border border-border bg-background pl-10 pr-9 text-sm text-foreground placeholder:text-muted-foreground focus:border-foreground focus:outline-none"
        />
        {search && (
          <button onClick={() => setSearch("")} className="absolute right-2.5 top-1/2 -translate-y-1/2 rounded-full p-0.5 text-muted-foreground hover:text-foreground" aria-label="Clear search">
            <X className="h-3.5 w-3.5" />
          </button>
        )}
      </div>

      {categories && categories.length > 0 && (
        <div>
          <button onClick={() => toggleSection("categories")} className="flex w-full items-center justify-between text-sm font-semibold text-foreground">
            Categories
            <ChevronRight className={"h-4 w-4 text-muted-foreground transition-transform duration-200 " + (openSections.categories ? "rotate-90" : "")} />
          </button>
          <div className={"mt-3 space-y-0.5 " + (openSections.categories ? "" : "hidden")}>
            <button
              onClick={() => setSelectedCategory(null)}
              className={"flex w-full items-center justify-between rounded-lg px-3 py-2 text-left text-sm transition-colors " + (selectedCategory === null ? "bg-muted font-medium text-foreground" : "text-muted-foreground hover:text-foreground")}
            >
              <span>All Categories</span>
              <span className="text-xs text-muted-foreground/50">{(products ?? []).length}</span>
            </button>
            {categories.map((cat) => {
              var catCount = (products ?? []).filter((p) => p.category === cat.name).length;
              return (
                <button
                  key={cat.id}
                  onClick={() => setSelectedCategory(selectedCategory === cat.slug ? null : cat.slug)}
                  className={"flex w-full items-center justify-between rounded-lg px-3 py-2 text-left text-sm transition-colors " + (selectedCategory === cat.slug ? "bg-muted font-medium text-foreground" : "text-muted-foreground hover:text-foreground")}
                >
                  <span>{cat.name}</span>
                  <span className="text-xs text-muted-foreground/50">{catCount}</span>
                </button>
              );
            })}
          </div>
        </div>
      )}

      <div>
        <button onClick={() => toggleSection("price")} className="flex w-full items-center justify-between text-sm font-semibold text-foreground">
          Price Range
          <ChevronRight className={"h-4 w-4 text-muted-foreground transition-transform duration-200 " + (openSections.price ? "rotate-90" : "")} />
        </button>
        <div className={"mt-3 flex items-center gap-2 " + (openSections.price ? "" : "hidden")}
          <input
            type="number"
            value={priceMin}
            onChange={(e) => setPriceMin(e.target.value)}
            placeholder="Min"
            min="0"
            className="h-10 w-full rounded-lg border border-border bg-background px-3 text-sm text-foreground placeholder:text-muted-foreground focus:border-foreground focus:outline-none [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
          />
          <span className="text-sm text-muted-foreground">\u2013</span>
          <input
            type="number"
            value={priceMax}
            onChange={(e) => setPriceMax(e.target.value)}
            placeholder="Max"
            min="0"
            className="h-10 w-full rounded-lg border border-border bg-background px-3 text-sm text-foreground placeholder:text-muted-foreground focus:border-foreground focus:outline-none [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
          />
        </div>
      </div>

      <div>
        <button onClick={() => toggleSection("filters")} className="flex w-full items-center justify-between text-sm font-semibold text-foreground">
          Filters
          <ChevronRight className={"h-4 w-4 text-muted-foreground transition-transform duration-200 " + (openSections.filters ? "rotate-90" : "")} />
        </button>
        <div className={"mt-3 space-y-3 " + (openSections.filters ? "" : "hidden")}
          <label className="flex cursor-pointer items-center gap-3">
            <input
              type="checkbox"
              checked={onSaleOnly}
              onChange={(e) => setOnSaleOnly(e.target.checked)}
              className="h-4 w-4 rounded border-border accent-foreground"
            />
            <span className="text-sm text-muted-foreground">On Sale</span>
          </label>
          <label className="flex cursor-pointer items-center gap-3">
            <input
              type="checkbox"
              checked={inStockOnly}
              onChange={(e) => setInStockOnly(e.target.checked)}
              className="h-4 w-4 rounded border-border accent-foreground"
            />
            <span className="text-sm text-muted-foreground">In Stock</span>
          </label>
        </div>
      </div>

      {activeFilterCount > 0 && (
        <button
          onClick={clearFilters}
          className="text-sm text-muted-foreground underline underline-offset-4 transition-colors hover:text-foreground"
        >
          Clear all filters
        </button>
      )}
    </div>
  );

  return (
    <div className="mx-auto max-w-7xl px-4 py-8 sm:px-6 sm:py-12 lg:px-8">
      {/* Page Header */}
      <div className="mb-8">
        <h1 className="text-3xl font-bold tracking-tight text-foreground sm:text-4xl">
          All Products
        </h1>
        {!isLoading && (
          <p className="mt-1 text-sm text-muted-foreground">
            {"Showing " + filtered.length + " / " + (products?.length ?? 0) + " products"}
          </p>
        )}
      </div>

      {/* Toolbar */}
      <div className="mb-6 flex items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <button
            onClick={() => setShowFilters(true)}
            className="inline-flex h-10 items-center gap-2 rounded-lg border border-border bg-background px-4 text-sm font-medium text-foreground transition-colors hover:bg-muted lg:hidden"
          >
            <SlidersHorizontal className="h-4 w-4" />
            Filters
            {activeFilterCount > 0 && (
              <span className="flex h-5 w-5 items-center justify-center rounded-full bg-foreground text-[10px] font-semibold text-background">
                {activeFilterCount}
              </span>
            )}
          </button>

          <div className="relative">
            <button
              onClick={() => setShowSort(!showSort)}
              className="inline-flex h-10 items-center gap-2 rounded-lg border border-border bg-background px-4 text-sm font-medium text-foreground transition-colors hover:bg-muted"
            >
              {sortLabels[sort]}
              <ChevronDown className={"h-3.5 w-3.5 transition-transform " + (showSort ? "rotate-180" : "")} />
            </button>
            <AnimatePresence>
              {showSort && (
                <>
                  <div className="fixed inset-0 z-10" onClick={() => setShowSort(false)} />
                  <motion.div
                    initial={{ opacity: 0, y: 4 }}
                    animate={{ opacity: 1, y: 0 }}
                    exit={{ opacity: 0, y: 4 }}
                    transition={{ duration: 0.15 }}
                    className="absolute left-0 top-full z-20 mt-1 w-48 rounded-xl border border-border/40 bg-background py-1 shadow-lg"
                  >
                    {(Object.entries(sortLabels) as [SortOption, string][]).map(([key, label]) => (
                      <button
                        key={key}
                        onClick={() => { setSort(key); setShowSort(false); }}
                        className={"w-full px-4 py-2 text-left text-sm transition-colors hover:bg-muted " + (sort === key ? "font-medium text-foreground" : "text-muted-foreground")}
                      >
                        {label}
                      </button>
                    ))}
                  </motion.div>
                </>
              )}
            </AnimatePresence>
          </div>
        </div>

        <div className="flex overflow-hidden rounded-lg border border-border">
          <button
            onClick={() => setViewMode("grid")}
            className={"flex h-9 w-9 items-center justify-center transition-colors " + (viewMode === "grid" ? "bg-foreground text-background" : "text-muted-foreground hover:text-foreground")}
            aria-label="Grid view"
          >
            <LayoutGrid className="h-4 w-4" />
          </button>
          <button
            onClick={() => setViewMode("list")}
            className={"flex h-9 w-9 items-center justify-center transition-colors " + (viewMode === "list" ? "bg-foreground text-background" : "text-muted-foreground hover:text-foreground")}
            aria-label="List view"
          >
            <List className="h-4 w-4" />
          </button>
        </div>
      </div>

      {/* Active Filter Chips */}
      {activeFilterCount > 0 && (
        <div className="mb-4 flex flex-wrap items-center gap-2">
          {selectedCategory && (
            <button
              onClick={() => setSelectedCategory(null)}
              className="inline-flex items-center gap-1 rounded-full bg-foreground/10 px-3 py-1.5 text-xs font-medium text-foreground transition-colors hover:bg-foreground/20"
            >
              {categories?.find((c) => c.slug === selectedCategory)?.name || selectedCategory}
              <X className="h-3 w-3" />
            </button>
          )}
          {priceMin && (
            <button onClick={() => setPriceMin("")} className="inline-flex items-center gap-1 rounded-full bg-foreground/10 px-3 py-1.5 text-xs font-medium text-foreground transition-colors hover:bg-foreground/20">
              {"Min: $" + priceMin} <X className="h-3 w-3" />
            </button>
          )}
          {priceMax && (
            <button onClick={() => setPriceMax("")} className="inline-flex items-center gap-1 rounded-full bg-foreground/10 px-3 py-1.5 text-xs font-medium text-foreground transition-colors hover:bg-foreground/20">
              {"Max: $" + priceMax} <X className="h-3 w-3" />
            </button>
          )}
          {onSaleOnly && (
            <button onClick={() => setOnSaleOnly(false)} className="inline-flex items-center gap-1 rounded-full bg-rose-100 px-3 py-1.5 text-xs font-medium text-rose-700 transition-colors hover:bg-rose-200 dark:bg-rose-500/10 dark:text-rose-400">
              On Sale <X className="h-3 w-3" />
            </button>
          )}
          {inStockOnly && (
            <button onClick={() => setInStockOnly(false)} className="inline-flex items-center gap-1 rounded-full bg-foreground/10 px-3 py-1.5 text-xs font-medium text-foreground transition-colors hover:bg-foreground/20">
              In Stock <X className="h-3 w-3" />
            </button>
          )}
          <button onClick={clearFilters} className="text-xs text-muted-foreground underline underline-offset-4 transition-colors hover:text-foreground">
            Clear all
          </button>
        </div>
      )}

      {/* Main: Sidebar + Products */}
      <div className="flex gap-8">
        <aside className="hidden w-60 shrink-0 lg:block">
          {filterContent}
        </aside>

        <div className="min-w-0 flex-1">
          {isLoading ? (
            <div className={"grid gap-6 " + (viewMode === "grid" ? "grid-cols-2 sm:gap-8 lg:grid-cols-3" : "grid-cols-1")}>
              {Array.from({ length: 6 }).map((_, i) => (
                <div key={i} className={viewMode === "grid" ? "space-y-3" : "flex gap-4 rounded-2xl border border-border/40 p-3"}>
                  <Skeleton className={viewMode === "grid" ? "aspect-[3/4] w-full rounded-2xl" : "h-36 w-36 shrink-0 rounded-xl"} />
                  <div className={viewMode === "grid" ? "" : "flex-1 space-y-2 py-2"}>
                    <Skeleton className="h-4 w-3/4" />
                    <Skeleton className="h-4 w-1/2" />
                  </div>
                </div>
              ))}
            </div>
          ) : filtered.length === 0 ? (
            <EmptyState
              icon={<Search className="h-10 w-10" />}
              title="No products found"
              description={search || activeFilterCount > 0 ? "Try adjusting your search or filters." : "Check back soon for new arrivals."}
              action={(search || activeFilterCount > 0) ? (
                <button
                  onClick={() => { setSearch(""); clearFilters(); }}
                  className="inline-flex h-11 items-center rounded-full bg-foreground px-6 text-sm font-medium text-background transition-opacity hover:opacity-90"
                >
                  Clear Filters
                </button>
              ) : undefined}
            />
          ) : viewMode === "grid" ? (
            <motion.div
              variants={containerVariants}
              initial="hidden"
              animate="show"
              className="grid grid-cols-2 gap-6 sm:gap-8 lg:grid-cols-3"
            >
              {filtered.map((product) => (
                <motion.div key={product.id} variants={itemVariants}>
                  <ProductCard product={product} />
                </motion.div>
              ))}
            </motion.div>
          ) : (
            <motion.div variants={containerVariants} initial="hidden" animate="show" className="space-y-3">
              {filtered.map((product) => {
                const onSale = product.compare_at_price > 0 && product.compare_at_price > product.price_in_cents;
                return (
                  <motion.div key={product.id} variants={itemVariants}>
                    <Link
                      href={"/products/" + product.slug}
                      className="group flex gap-5 rounded-2xl border border-border/40 bg-background p-3 transition-shadow hover:shadow-md"
                    >
                      <div className="relative h-36 w-36 shrink-0 overflow-hidden rounded-xl bg-muted sm:h-44 sm:w-44">
                        {product.images && product.images.length > 0 ? (
                          <Image
                            src={product.images[0]}
                            alt={product.name}
                            fill
                            className="object-cover transition-transform duration-300 group-hover:scale-105"
                            sizes="176px"
                          />
                        ) : (
                          <div className="flex h-full w-full items-center justify-center">
                            <ShoppingBag className="h-8 w-8 text-muted-foreground/40" />
                          </div>
                        )}
                        {onSale && (
                          <div className="absolute left-2 top-2 rounded-full bg-foreground px-2 py-0.5 text-[10px] font-medium text-background">
                            Sale
                          </div>
                        )}
                      </div>
                      <div className="flex min-w-0 flex-1 flex-col justify-between py-1">
                        <div>
                          {product.category && (
                            <p className="text-[11px] font-medium uppercase tracking-[0.15em] text-muted-foreground/60">
                              {product.category}
                            </p>
                          )}
                          <h3 className="mt-1 text-base font-semibold text-foreground group-hover:underline">
                            {product.name}
                          </h3>
                          {product.description && (
                            <p className="mt-1.5 line-clamp-2 text-sm text-muted-foreground">
                              {product.description}
                            </p>
                          )}
                        </div>
                        <div className="mt-3 flex items-center gap-3">
                          <span className="text-base font-semibold text-foreground">{formatPrice(product.price_in_cents)}</span>
                          {onSale && (
                            <span className="text-sm text-muted-foreground line-through">{formatPrice(product.compare_at_price)}</span>
                          )}
                          {product.in_stock === false && (
                            <span className="rounded-full bg-muted px-2.5 py-0.5 text-xs font-medium text-muted-foreground">Out of Stock</span>
                          )}
                        </div>
                      </div>
                    </Link>
                  </motion.div>
                );
              })}
            </motion.div>
          )}
        </div>
      </div>

      {/* Mobile Filter Drawer */}
      <AnimatePresence>
        {showFilters && (
          <>
            <motion.div
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              className="fixed inset-0 z-40 bg-black/40"
              onClick={() => setShowFilters(false)}
            />
            <motion.div
              initial={{ x: "-100%" }}
              animate={{ x: 0 }}
              exit={{ x: "-100%" }}
              transition={{ type: "spring", damping: 25, stiffness: 300 }}
              className="fixed inset-y-0 left-0 z-50 w-80 max-w-[85vw] overflow-y-auto bg-background p-6 shadow-xl"
            >
              <div className="mb-6 flex items-center justify-between">
                <h2 className="text-lg font-semibold text-foreground">Filters</h2>
                <button
                  onClick={() => setShowFilters(false)}
                  className="flex h-8 w-8 items-center justify-center rounded-full text-muted-foreground transition-colors hover:text-foreground"
                >
                  <X className="h-5 w-5" />
                </button>
              </div>
              {filterContent}
            </motion.div>
          </>
        )}
      </AnimatePresence>
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_PRODUCT_DETAIL_PAGE = `"use client";

import React, { useState } from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import { Minus, Plus, ShoppingCart, Truck, RotateCcw, Shield, Check, Share2, Copy } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { useProduct, useProducts, useCart, formatPrice } from "@/lib/wemob-store";
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
          <div className="mt-8 space-y-4">
            <div className="grid grid-cols-3 gap-3">
              <div className="flex flex-col items-center gap-2 rounded-xl border border-border/40 p-3 text-center">
                <Truck className="h-5 w-5 text-muted-foreground" />
                <div>
                  <p className="text-xs font-medium text-foreground">Free Shipping</p>
                  <p className="text-[10px] text-muted-foreground">Orders over $50</p>
                </div>
              </div>
              <div className="flex flex-col items-center gap-2 rounded-xl border border-border/40 p-3 text-center">
                <RotateCcw className="h-5 w-5 text-muted-foreground" />
                <div>
                  <p className="text-xs font-medium text-foreground">Easy Returns</p>
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

            {/* Guaranteed Safe Checkout */}
            <div className="rounded-xl border border-border/40 p-4">
              <p className="mb-2.5 text-center text-[11px] font-medium uppercase tracking-[0.15em] text-muted-foreground/60">Guaranteed Safe Checkout</p>
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
          <h2 className="text-2xl font-semibold tracking-tight text-foreground">You May Also Like</h2>
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
`

const ECOMMERCE_TEMPLATE_CATEGORY_PAGE = `"use client";

import React, { useState, useMemo } from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import { ShoppingBag, ChevronDown } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
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

type CatSort = "featured" | "price-asc" | "price-desc" | "name";

export default function CategoryPage() {
  const params = useParams();
  const slug = params?.slug as string;
  const { products, isLoading: productsLoading } = useProducts();
  const { categories, isLoading: categoriesLoading } = useCategories();
  const [sort, setSort] = useState<CatSort>("featured");
  const [showSort, setShowSort] = useState(false);

  const sortLabels: Record<CatSort, string> = {
    featured: "Featured",
    "price-asc": "Price: Low to High",
    "price-desc": "Price: High to Low",
    name: "Name A\u2013Z",
  };

  const isLoading = productsLoading || categoriesLoading;
  const category = categories?.find((c) => c.slug === slug);
  const categoryProducts = products?.filter(
    (p) => p.category === category?.name
  ) ?? [];

  const filtered = useMemo(() => {
    var result = [...categoryProducts];
    switch (sort) {
      case "price-asc": return result.sort((a, b) => a.price_in_cents - b.price_in_cents);
      case "price-desc": return result.sort((a, b) => b.price_in_cents - a.price_in_cents);
      case "name": return result.sort((a, b) => a.name.localeCompare(b.name));
      default: return result;
    }
  }, [categoryProducts, sort]);

  const siblingCategories = categories?.filter((c) => c.slug !== slug).slice(0, 6) ?? [];

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

      <div className="mb-10 rounded-2xl bg-gradient-to-br from-muted/50 to-muted/20 p-8 sm:p-10">
        <h1 className="text-4xl font-semibold tracking-tight text-foreground sm:text-5xl">
          {categoryName}
        </h1>
        {category?.description && (
          <p className="mt-3 max-w-2xl text-muted-foreground">{category.description}</p>
        )}
        <p className="mt-2 text-sm text-muted-foreground">
          {filtered.length} {filtered.length === 1 ? "product" : "products"}
        </p>
      </div>

      {/* Browse other categories */}
      {siblingCategories.length > 0 && (
        <div className="mb-8 flex flex-wrap gap-2">
          {siblingCategories.map((c) => (
            <Link
              key={c.id}
              href={"/categories/" + c.slug}
              className="rounded-full border border-border/60 px-4 py-1.5 text-sm text-muted-foreground transition-colors hover:border-foreground hover:text-foreground"
            >
              {c.name}
            </Link>
          ))}
        </div>
      )}

      {/* Sort dropdown */}
      {filtered.length > 1 && (
        <div className="mb-6 flex justify-end">
          <div className="relative">
            <button
              onClick={() => setShowSort(!showSort)}
              className="inline-flex h-10 items-center gap-2 rounded-lg border border-border bg-background px-4 text-sm font-medium text-foreground transition-colors hover:bg-muted"
            >
              {sortLabels[sort]}
              <ChevronDown className={"h-3.5 w-3.5 transition-transform " + (showSort ? "rotate-180" : "")} />
            </button>
            <AnimatePresence>
              {showSort && (
                <>
                  <div className="fixed inset-0 z-10" onClick={() => setShowSort(false)} />
                  <motion.div
                    initial={{ opacity: 0, y: 4 }}
                    animate={{ opacity: 1, y: 0 }}
                    exit={{ opacity: 0, y: 4 }}
                    transition={{ duration: 0.15 }}
                    className="absolute right-0 top-full z-20 mt-1 w-48 rounded-xl border border-border/40 bg-background py-1 shadow-lg"
                  >
                    {(Object.entries(sortLabels) as [CatSort, string][]).map(([key, label]) => (
                      <button
                        key={key}
                        onClick={() => { setSort(key); setShowSort(false); }}
                        className={"w-full px-4 py-2 text-left text-sm transition-colors hover:bg-muted " + (sort === key ? "font-medium text-foreground" : "text-muted-foreground")}
                      >
                        {label}
                      </button>
                    ))}
                  </motion.div>
                </>
              )}
            </AnimatePresence>
          </div>
        </div>
      )}

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
  var toSlug = function(s: string) { return s.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/(^-|-$)/g, ""); };

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

      <nav className="mt-8 flex flex-wrap gap-2" aria-label="Quick links">
        <a href="#shipping-rates-delivery-times" className="rounded-full border border-border/40 px-3 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-muted/30 hover:text-foreground">Shipping Rates</a>
        {sections.map((s) => (
          <a key={s.title} href={"#" + toSlug(s.title)} className="rounded-full border border-border/40 px-3 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-muted/30 hover:text-foreground">{s.title}</a>
        ))}
      </nav>

      <div className="mt-10 space-y-6">
        {/* Shipping rates table */}
        <motion.div
          id="shipping-rates-delivery-times"
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
            id={toSlug(section.title)}
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

        <div className="mt-4 rounded-2xl border border-border/40 p-6 text-center">
          <p className="text-sm text-muted-foreground">Have questions about our shipping policy?</p>
          <a href="/contact" className="mt-2 inline-flex items-center text-sm font-medium text-foreground underline underline-offset-4 transition-colors hover:text-foreground/80">Contact Us</a>
        </div>
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
  var toSlug = function(s: string) { return s.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/(^-|-$)/g, ""); };

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

      <nav className="mt-8 flex flex-wrap gap-2" aria-label="Quick links">
        {sections.map((s) => (
          <a key={s.title} href={"#" + toSlug(s.title)} className="rounded-full border border-border/40 px-3 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-muted/30 hover:text-foreground">{s.title}</a>
        ))}
      </nav>

      <div className="mt-10 space-y-6">
        {sections.map((section, idx) => (
          <motion.section
            key={section.title}
            id={toSlug(section.title)}
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

        <div className="mt-4 rounded-2xl border border-border/40 p-6 text-center">
          <p className="text-sm text-muted-foreground">Need to start a return?</p>
          <a href="/contact" className="mt-2 inline-flex items-center text-sm font-medium text-foreground underline underline-offset-4 transition-colors hover:text-foreground/80">Contact Our Support Team</a>
        </div>
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
  var toSlug = function(s: string) { return s.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/(^-|-$)/g, ""); };

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

      <nav className="mt-8 flex flex-wrap gap-2" aria-label="Quick links">
        {sections.map((s) => (
          <a key={s.title} href={"#" + toSlug(s.title)} className="rounded-full border border-border/40 px-3 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-muted/30 hover:text-foreground">{s.title}</a>
        ))}
      </nav>

      <div className="mt-10 space-y-6">
        {sections.map((section, idx) => (
          <motion.section
            key={section.title + idx}
            id={toSlug(section.title)}
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

        <div className="mt-4 rounded-2xl border border-border/40 p-6 text-center">
          <p className="text-sm text-muted-foreground">Have questions about your privacy or data?</p>
          <a href="/contact" className="mt-2 inline-flex items-center text-sm font-medium text-foreground underline underline-offset-4 transition-colors hover:text-foreground/80">Contact Us</a>
        </div>
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
  var toSlug = function(s: string) { return s.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/(^-|-$)/g, ""); };

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

      <nav className="mt-8 flex flex-wrap gap-2" aria-label="Quick links">
        {sections.map((s) => (
          <a key={s.title} href={"#" + toSlug(s.title)} className="rounded-full border border-border/40 px-3 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-muted/30 hover:text-foreground">{s.title}</a>
        ))}
      </nav>

      <div className="mt-10 space-y-6">
        {sections.map((section, idx) => (
          <motion.section
            key={section.title}
            id={toSlug(section.title)}
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

        <div className="mt-4 rounded-2xl border border-border/40 p-6 text-center">
          <p className="text-sm text-muted-foreground">Have questions about our terms of service?</p>
          <a href="/contact" className="mt-2 inline-flex items-center text-sm font-medium text-foreground underline underline-offset-4 transition-colors hover:text-foreground/80">Contact Us</a>
        </div>
      </div>
    </div>
  );
}
`
