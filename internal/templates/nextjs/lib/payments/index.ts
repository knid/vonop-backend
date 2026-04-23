import "server-only";
// lib/payments/index.ts — Payment provider dispatch + shared types.
// This file is template-managed. Do not modify.
//
// Each provider adapter exposes the same `createSession` + `handleWebhook`
// pair via `PaymentAdapter`. Callers pick the adapter with `getAdapter(provider)`
// using the `payment_provider` column on the order (or the configured default
// provider) and avoid branching on provider strings elsewhere.

import type { Order, Config } from "@/lib/schema";

import * as stripe from "./stripe";
import * as iyzico from "./iyzico";
import * as cod from "./cod";

export type CheckoutResult =
  | { kind: "redirect"; url: string }
  | { kind: "client_confirm"; clientSecret: string }
  | { kind: "placed" }; // COD / no-payment flows

export type WebhookEvent = {
  orderId: string;
  paymentId: string;
  status: "paid" | "failed" | "pending";
  provider: string;
};

// Per-request context the checkout route threads into adapters. Kept separate
// from `Order` so fields that don't belong on the DB row (clientIp, viewToken
// is on metadata) stay out of the schema.
export type CheckoutContext = {
  clientIp: string; // best-effort from x-forwarded-for; "" if unresolved
};

export type PaymentAdapter = {
  createSession: (
    order: Order,
    config: Config,
    ctx: CheckoutContext,
  ) => Promise<CheckoutResult>;
  handleWebhook: (rawBody: string, headers: Headers) => Promise<WebhookEvent>;
};

const adapters: Record<string, PaymentAdapter> = {
  stripe,
  iyzico,
  cod,
};

export function getAdapter(provider: string): PaymentAdapter {
  const adapter = adapters[provider];
  if (!adapter) throw new Error(`Unsupported payment provider: ${provider}`);
  return adapter;
}
