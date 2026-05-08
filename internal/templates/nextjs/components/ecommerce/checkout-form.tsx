"use client";

import React from "react";
import { Loader2, AlertTriangle, Mail, MapPin, CreditCard, ShieldCheck } from "lucide-react";
import { useCart, useStoreConfig, formatPrice } from "@/lib/wemob-store";
import { OrderSummary } from "@/components/ecommerce/order-summary";

// Shape returned by the local /api/checkout route handler. Kept inline because
// the payments module is server-only and can't be imported from a client
// component. viewToken is the per-order IDOR gate for /checkout/success and
// /checkout/cancel pages.
type CheckoutResult =
  | { orderId: string; viewToken: string; kind: "redirect"; url: string }
  | { orderId: string; viewToken: string; kind: "client_confirm"; clientSecret: string }
  | { orderId: string; viewToken: string; kind: "placed" };

export function CheckoutForm() {
  const { cart, isLoading: cartLoading } = useCart();
  const { config, isLoading: configLoading } = useStoreConfig();
  const [isSubmitting, setIsSubmitting] = React.useState(false);
  const [error, setError] = React.useState("");
  const [selectedProvider, setSelectedProvider] = React.useState("");

  const [form, setForm] = React.useState({
    email: "",
    name: "",
    phone: "",
    line1: "",
    line2: "",
    city: "",
    state: "",
    postal_code: "",
    country: "",
    identity_number: "",
  });

  const providers = config?.payment_providers || [];
  // Show TR-specific fields whenever the store is configured for Turkey: by
  // locale OR by currency. Iyzico's checkoutFormInitialize rejects every
  // call without a buyer.gsmNumber, so we have to collect a phone for any
  // store the merchant has pointed at TR — even if locale was left at "en"
  // because the AI seeded the store before that prompt-addendum landed.
  const isTurkishStore =
    config?.locale === "tr" || config?.currency === "TRY";
  const showIdentityField = isTurkishStore;
  const showPhoneField = isTurkishStore;

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

    const shippingAddress = {
      line1: form.line1,
      line2: form.line2 || undefined,
      city: form.city,
      state: form.state,
      postal_code: form.postal_code,
      country: form.country,
    };

    try {
      const res = await fetch("/api/checkout", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          provider: selectedProvider || "cod",
          email: form.email,
          name: form.name,
          phone: form.phone || undefined,
          shippingAddress,
          identityNumber: form.identity_number || undefined,
          // billingAddress omitted — server defaults to shippingAddress
        }),
      });

      if (!res.ok) {
        const err = await res.json().catch(() => ({}));
        const ERROR_KEYS: Record<string, string> = {
          missing_fields: "{{t.errorMissingFields}}",
          no_cart_session: "{{t.errorNoCartSession}}",
          no_config: "{{t.errorNoConfig}}",
          cart_not_found: "{{t.errorCartNotFound}}",
          empty_cart: "{{t.errorEmptyCart}}",
          product_unavailable: "{{t.errorProductUnavailable}}",
          invalid_quantity: "{{t.errorInvalidQuantity}}",
          insufficient_stock: "{{t.errorInsufficientStock}}",
          order_insert_failed: "{{t.errorOrderInsertFailed}}",
          payment_init_failed: "{{t.errorPaymentInitFailed}}",
        };
        const code = err.error ?? "";
        setError(ERROR_KEYS[code] ?? "{{t.checkoutFailed}}");
        setIsSubmitting(false);
        return;
      }

      const result = (await res.json()) as CheckoutResult;

      if (result.kind === "redirect") {
        window.location.href = result.url;
      } else if (result.kind === "client_confirm") {
        // Reserved for Stripe Elements / PaymentIntent confirm flow. In PR 3
        // adapters return "redirect" for Stripe (Checkout Session) and
        // "placed" for COD, so this branch shouldn't fire yet.
        setError("{{t.checkoutFailed}}");
        console.error("client_confirm not implemented in this template", result);
        setIsSubmitting(false);
      } else if (result.kind === "placed") {
        window.location.href =
          "/checkout/success?order=" + result.orderId + "&t=" + result.viewToken;
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "{{t.checkoutFailed}}");
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
        <div className="rounded-2xl bg-muted/20 p-7">
          <div className="mb-5 flex items-center gap-2">
            <Mail className="h-4 w-4 text-muted-foreground" />
            <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">{{t.contactInformation}}</h2>
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="sm:col-span-2">
              <label htmlFor="email" className="mb-1.5 block text-xs uppercase tracking-wider text-muted-foreground">{{t.email}}</label>
              <input id="email" name="email" type="email" required value={form.email} onChange={handleChange} className={inputClass} />
            </div>
            <div className="sm:col-span-2">
              <label htmlFor="name" className="mb-1.5 block text-xs uppercase tracking-wider text-muted-foreground">{{t.fullName}}</label>
              <input id="name" name="name" type="text" required value={form.name} onChange={handleChange} className={inputClass} />
            </div>
            {showPhoneField && (
              <div className="sm:col-span-2">
                <label htmlFor="phone" className="mb-1.5 block text-xs uppercase tracking-wider text-muted-foreground">{`{{t.phone}}`}</label>
                <input
                  id="phone"
                  name="phone"
                  type="tel"
                  inputMode="tel"
                  autoComplete="tel"
                  required
                  value={form.phone}
                  onChange={handleChange}
                  className={inputClass}
                  placeholder="+905XXXXXXXXX"
                />
                <p className="mt-1 text-xs text-muted-foreground/70">{`{{t.phoneHelp}}`}</p>
              </div>
            )}
          </div>
        </div>

        <div className="rounded-2xl bg-muted/20 p-7">
          <div className="mb-5 flex items-center gap-2">
            <MapPin className="h-4 w-4 text-muted-foreground" />
            <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">{{t.shippingAddress}}</h2>
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="sm:col-span-2">
              <label htmlFor="line1" className="mb-1.5 block text-xs uppercase tracking-wider text-muted-foreground">{{t.addressLine1}}</label>
              <input id="line1" name="line1" type="text" required value={form.line1} onChange={handleChange} className={inputClass} />
            </div>
            <div className="sm:col-span-2">
              <label htmlFor="line2" className="mb-1.5 block text-xs uppercase tracking-wider text-muted-foreground">{{t.addressLine2Optional}}</label>
              <input id="line2" name="line2" type="text" value={form.line2} onChange={handleChange} className={inputClass} />
            </div>
            <div>
              <label htmlFor="city" className="mb-1.5 block text-xs uppercase tracking-wider text-muted-foreground">{{t.city}}</label>
              <input id="city" name="city" type="text" required value={form.city} onChange={handleChange} className={inputClass} />
            </div>
            <div>
              <label htmlFor="state" className="mb-1.5 block text-xs uppercase tracking-wider text-muted-foreground">{{t.stateProvince}}</label>
              <input id="state" name="state" type="text" required value={form.state} onChange={handleChange} className={inputClass} />
            </div>
            <div>
              <label htmlFor="postal_code" className="mb-1.5 block text-xs uppercase tracking-wider text-muted-foreground">{{t.postalCode}}</label>
              <input id="postal_code" name="postal_code" type="text" required value={form.postal_code} onChange={handleChange} className={inputClass} />
            </div>
            <div>
              <label htmlFor="country" className="mb-1.5 block text-xs uppercase tracking-wider text-muted-foreground">{{t.country}}</label>
              <input id="country" name="country" type="text" required value={form.country} onChange={handleChange} className={inputClass} />
            </div>
            {showIdentityField && (
              <div className="sm:col-span-2">
                <label htmlFor="identity_number" className="mb-1.5 block text-xs uppercase tracking-wider text-muted-foreground">{`{{t.identityNumber}}`}</label>
                <input
                  id="identity_number"
                  name="identity_number"
                  type="text"
                  inputMode="numeric"
                  pattern="\d{11}"
                  maxLength={11}
                  value={form.identity_number}
                  onChange={handleChange}
                  className={inputClass}
                  placeholder="11111111111"
                />
              </div>
            )}
          </div>
        </div>

        {providers.length > 0 ? (
          <div className="rounded-2xl bg-muted/20 p-7">
            <div className="mb-5 flex items-center gap-2">
              <CreditCard className="h-4 w-4 text-muted-foreground" />
              <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">{{t.paymentMethod}}</h2>
            </div>
            <div className="space-y-3">
              {providers.map((p) => (
                <label key={p} className={"flex cursor-pointer items-center gap-3 rounded-xl border p-4 transition-colors " + (selectedProvider === p ? "border-foreground bg-foreground/5" : "border-border/40 hover:bg-muted/30")}>
                  <input type="radio" name="payment" value={p} checked={selectedProvider === p} onChange={() => setSelectedProvider(p)} className="h-4 w-4 accent-foreground" />
                  <span className="text-sm font-medium capitalize">{p === "cod" ? "{{t.cashOnDelivery}}" : p}</span>
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
          {isSubmitting ? "{{t.processing}}" : "{{t.placeOrder}}" + (cart ? " · " + formatPrice(cart.total_cents) : "")}
        </button>

        <div className="flex items-center justify-center gap-1.5 text-xs text-muted-foreground/60">
          <ShieldCheck className="h-3.5 w-3.5" />
          <span>{{t.infoEncryptedSecure}}</span>
        </div>
      </form>

      <div className="lg:sticky lg:top-4 lg:self-start">
        <OrderSummary cart={cart} />
      </div>
    </div>
  );
}
