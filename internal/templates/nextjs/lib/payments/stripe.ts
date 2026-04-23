import "server-only";
// lib/payments/stripe.ts — Stripe Checkout adapter.
// This file is template-managed. Do not modify.
//
// Env vars (mapped by backend integrations.go):
//   STRIPE_SK               — secret key
//   NEXT_PUBLIC_STRIPE_PK   — publishable key (browser-only, unused here)
//   STRIPE_WEBHOOK_SECRET   — signing secret for webhook signature verification

import Stripe from "stripe";

import type { Order, Config } from "@/lib/schema";

import type { CheckoutResult, WebhookEvent } from "./index";

function getClient(): Stripe {
  const sk = process.env.STRIPE_SK;
  if (!sk) throw new Error("STRIPE_SK not set");
  return new Stripe(sk, { apiVersion: "2024-12-18.acacia" });
}

export async function createSession(
  order: Order,
  config: Config,
): Promise<CheckoutResult> {
  const stripe = getClient();
  const siteUrl = process.env.NEXT_PUBLIC_SITE_URL ?? "";

  const session = await stripe.checkout.sessions.create({
    mode: "payment",
    line_items: [
      {
        quantity: 1,
        price_data: {
          currency: config.currency.toLowerCase(),
          unit_amount: Number(order.totalCents),
          product_data: { name: `Order ${order.orderNumber}` },
        },
      },
    ],
    metadata: { order_id: order.id },
    success_url: `${siteUrl}/checkout/success?order=${order.id}`,
    cancel_url: `${siteUrl}/checkout/cancel?order=${order.id}`,
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
