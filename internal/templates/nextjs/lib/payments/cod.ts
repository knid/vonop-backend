import "server-only";
// lib/payments/cod.ts — Cash-on-delivery adapter.
// This file is template-managed. Do not modify.
//
// COD has no upstream provider. `createSession` returns `placed` so the
// order route handler can skip a redirect and mark the order as pending
// collection directly. `handleWebhook` is never called — COD has no webhooks.

import type { Order, Config } from "@/lib/schema";

import type { CheckoutResult, WebhookEvent } from "./index";

export async function createSession(
  _order: Order,
  _config: Config,
  _ctx: import("./index").CheckoutContext,
): Promise<CheckoutResult> {
  return { kind: "placed" };
}

export async function handleWebhook(
  _rawBody: string,
  _headers: Headers,
): Promise<WebhookEvent> {
  throw new Error("COD has no webhooks");
}
