import "server-only";
// lib/payments/stripe.ts — Stripe Checkout adapter.
// This file is template-managed. Do not modify.
//
// Env vars (mapped by backend integrations.go):
//   STRIPE_SK               — secret key
//   NEXT_PUBLIC_STRIPE_PK   — publishable key (browser-only, unused here)
//   STRIPE_WEBHOOK_SECRET   — signing secret for webhook signature verification

import Stripe from "stripe";
import { eq } from "drizzle-orm";

import { db, schema } from "@/lib/db";
import type { Order, Config } from "@/lib/schema";

import type { CheckoutContext, CheckoutResult, WebhookEvent } from "./index";

function getClient(): Stripe {
  const sk = process.env.STRIPE_SK;
  if (!sk) throw new Error("STRIPE_SK not set");
  return new Stripe(sk, { apiVersion: "2024-12-18.acacia", timeout: 15000 });
}

function resolveSiteUrl(raw: string | undefined): string {
  const trimmed = (raw ?? process.env.NEXT_PUBLIC_SITE_URL ?? "").trim();
  if (!trimmed) return "";
  const noSlash = trimmed.replace(/\/+$/, "");
  if (/^https?:\/\//i.test(noSlash)) return noSlash;
  // In case NEXT_PUBLIC_SITE_URL is set to bare host:port.
  return `https://${noSlash}`;
}

// Build Stripe line_items from order_items, showing each product at its
// real unit_amount and quantity. Any delta between subtotal and totalCents
// (tax + shipping - discount) is added as a single positive or negative
// adjustment line so the session total matches totalCents. Stripe rejects
// zero / negative unit_amount, so two edge cases fall back to a single
// opaque "Order total" line: (a) steep discounts that would round any
// per-item share to 0 cents; (b) no items. The caller must reject
// totalCents <= 0 before reaching here — Stripe Checkout sessions require
// a positive charge amount.
function buildLineItems(
  orderItems: Array<{ productName: string; quantity: number; priceInCents: number }>,
  currency: string,
  totalCents: number,
): Stripe.Checkout.SessionCreateParams.LineItem[] {
  const singleLine = (): Stripe.Checkout.SessionCreateParams.LineItem[] => [
    {
      quantity: 1,
      price_data: {
        currency,
        unit_amount: totalCents,
        product_data: { name: "Order total" },
      },
    },
  ];

  if (orderItems.length === 0) return singleLine();

  const lines: Stripe.Checkout.SessionCreateParams.LineItem[] = orderItems.map(
    (item) => ({
      quantity: item.quantity,
      price_data: {
        currency,
        unit_amount: Math.max(0, item.priceInCents),
        product_data: { name: item.productName },
      },
    }),
  );

  const subtotal = orderItems.reduce(
    (sum, i) => sum + i.priceInCents * i.quantity,
    0,
  );
  const delta = totalCents - subtotal;

  if (delta > 0) {
    lines.push({
      quantity: 1,
      price_data: {
        currency,
        unit_amount: delta,
        product_data: { name: "Tax & shipping" },
      },
    });
    return lines;
  }

  if (delta < 0) {
    // Discount case. Stripe doesn't accept negative unit_amount, so scale
    // each line's unit_amount down; the last line absorbs rounding drift so
    // the sum is exact. If any scaled unit_amount rounds to 0 (steep
    // discount + small per-item price), fall back to a single line — the
    // customer loses the itemized breakdown on Stripe's page but the
    // session is guaranteed to initialize.
    const ratio = totalCents / subtotal; // 0 < ratio < 1 (totalCents > 0 per caller contract)
    let allocated = 0;
    const scaled: number[] = [];
    for (let idx = 0; idx < orderItems.length; idx++) {
      const item = orderItems[idx];
      const isLast = idx === orderItems.length - 1;
      const lineTotal = isLast
        ? totalCents - allocated
        : Math.round(item.priceInCents * item.quantity * ratio);
      allocated += lineTotal;
      const unitAmount = Math.round(lineTotal / item.quantity);
      if (unitAmount <= 0) return singleLine();
      scaled.push(unitAmount);
    }
    for (let idx = 0; idx < lines.length; idx++) {
      lines[idx].price_data!.unit_amount = scaled[idx];
    }
  }

  return lines;
}

export async function createSession(
  order: Order,
  config: Config,
  ctx: CheckoutContext,
): Promise<CheckoutResult> {
  const totalCents = Number(order.totalCents);
  if (totalCents <= 0) {
    // Stripe Checkout requires a positive charge amount. A $0 order (100%
    // discount, or promo that fully covers the cart) should be handled by
    // the caller as a COD-style immediate placement instead of going
    // through a payment provider. Surface this as a hard error so the
    // failure is visible in logs — silently rerouting would hide a
    // pricing misconfiguration.
    throw new Error("Stripe session requires totalCents > 0");
  }

  const stripe = getClient();
  const siteUrl = resolveSiteUrl(ctx.siteUrl);
  if (!siteUrl) {
    throw new Error("Payment callback URL base not configured");
  }

  const items = await db
    .select({
      productName: schema.orderItems.productName,
      quantity: schema.orderItems.quantity,
      priceInCents: schema.orderItems.priceInCents,
    })
    .from(schema.orderItems)
    .where(eq(schema.orderItems.orderId, order.id));

  const viewToken =
    (order.metadata as Record<string, unknown> | null)?.viewToken ?? "";
  const tokenParam = viewToken ? `&t=${viewToken}` : "";

  const session = await stripe.checkout.sessions.create({
    mode: "payment",
    line_items: buildLineItems(
      items,
      config.currency.toLowerCase(),
      totalCents,
    ),
    metadata: { order_id: order.id },
    success_url: `${siteUrl}/checkout/success?order=${order.id}${tokenParam}`,
    cancel_url: `${siteUrl}/checkout/cancel?order=${order.id}${tokenParam}`,
  });

  return { kind: "redirect", url: session.url! };
}

export async function handleWebhook(
  rawBody: string,
  headers: Headers,
): Promise<WebhookEvent> {
  const stripe = getClient();
  const sig = headers.get("stripe-signature");
  const secret = process.env.STRIPE_WEBHOOK_SECRET;
  if (!sig || !secret) {
    throw new Error("stripe webhook secret/signature missing");
  }

  const event = stripe.webhooks.constructEvent(rawBody, sig, secret);

  if (event.type === "checkout.session.completed") {
    const session = event.data.object as Stripe.Checkout.Session;
    return {
      orderId: session.metadata?.order_id ?? "",
      paymentId: (session.payment_intent as string) ?? session.id,
      status: "paid",
      provider: "stripe",
    };
  }

  if (
    event.type === "checkout.session.expired" ||
    event.type === "payment_intent.payment_failed"
  ) {
    const obj = event.data.object as
      | Stripe.Checkout.Session
      | Stripe.PaymentIntent;
    const orderId =
      (obj as unknown as { metadata?: { order_id?: string } }).metadata
        ?.order_id ?? "";
    return {
      orderId,
      paymentId: (obj as unknown as { id?: string }).id ?? "",
      status: "failed",
      provider: "stripe",
    };
  }

  // Unhandled event types: return pending so callers can ignore safely.
  return { orderId: "", paymentId: "", status: "pending", provider: "stripe" };
}
