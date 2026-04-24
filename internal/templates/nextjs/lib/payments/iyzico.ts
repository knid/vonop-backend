import "server-only";
// lib/payments/iyzico.ts — Iyzico Checkout Form adapter.
// This file is template-managed. Do not modify.
//
// Env vars (mapped by backend integrations.go):
//   IYZICO_API_KEY     — merchant API key
//   IYZICO_SECRET_KEY  — merchant secret key
//   IYZICO_BASE_URL    — optional endpoint override; defaults to prod
//                        (https://api.iyzipay.com). Point at
//                        https://sandbox-api.iyzipay.com with sandbox keys
//                        for test charges.
//
// Flow:
//   1. createSession calls checkoutFormInitialize.create → paymentPageUrl
//   2. The user completes payment on Iyzico's hosted page
//   3. Iyzico posts `token` as form-encoded data to the callback URL
//   4. handleWebhook calls checkoutForm.retrieve({ token }) to confirm status
//
// The `iyzipay` module is loaded with a dynamic import inside getClient() so
// the package only enters the server bundle for routes that actually hit the
// Iyzico adapter.

import { eq } from "drizzle-orm";

import { db, schema } from "@/lib/db";
import type { Order, Config } from "@/lib/schema";

import type { CheckoutContext, CheckoutResult, WebhookEvent } from "./index";

type IyzipayCallback<T> = (err: unknown, res: T) => void;

type IyzipayClient = {
  checkoutFormInitialize: {
    create: (req: Record<string, unknown>, cb: IyzipayCallback<IyzipayInitResult>) => void;
  };
  checkoutForm: {
    retrieve: (req: Record<string, unknown>, cb: IyzipayCallback<IyzipayRetrieveResult>) => void;
  };
};

type IyzipayCtor = new (config: {
  apiKey: string;
  secretKey: string;
  uri: string;
}) => IyzipayClient;

type IyzipayInitResult = {
  status?: string;
  errorMessage?: string;
  paymentPageUrl?: string;
  checkoutFormContent?: string;
  token?: string;
};

type IyzipayRetrieveResult = {
  status?: string;
  paymentStatus?: string;
  paymentId?: string;
  conversationId?: string;
  errorMessage?: string;
};

async function getClient(): Promise<IyzipayClient> {
  const mod = (await import("iyzipay")) as unknown as {
    default: IyzipayCtor;
  };
  const Iyzipay = mod.default;
  const apiKey = process.env.IYZICO_API_KEY;
  const secretKey = process.env.IYZICO_SECRET_KEY;
  if (!apiKey || !secretKey) throw new Error("Iyzico keys not set");
  const uri = process.env.IYZICO_BASE_URL ?? "https://api.iyzipay.com";
  return new Iyzipay({ apiKey, secretKey, uri });
}

function promisify<T>(fn: (cb: IyzipayCallback<T>) => void): Promise<T> {
  return new Promise<T>((resolve, reject) =>
    fn((err, res) => (err ? reject(err) : resolve(res))),
  );
}

type AddressLike = {
  line1?: string;
  line2?: string;
  city?: string;
  country?: string;
  postal_code?: string;
};

export async function createSession(
  order: Order,
  config: Config,
  ctx: CheckoutContext,
): Promise<CheckoutResult> {
  const iyzipay = await getClient();
  const siteUrl = process.env.NEXT_PUBLIC_SITE_URL ?? "";

  const nameParts = (order.customerName ?? "").trim().split(/\s+/);
  const firstName = nameParts[0] || "Customer";
  const surname = nameParts.slice(1).join(" ") || firstName;

  const shipping = (order.shippingAddress ?? {}) as AddressLike;
  const billing = (order.billingAddress ?? order.shippingAddress ?? {}) as AddressLike;

  const shippingAddress = [shipping.line1, shipping.line2]
    .filter(Boolean)
    .join(" ") || "Not provided";
  const billingAddress = [billing.line1, billing.line2]
    .filter(Boolean)
    .join(" ") || shippingAddress;

  // Real line items drive Iyzico's fraud scoring, chargeback evidence, and
  // merchant reports. Left-join products → categories to surface a real
  // `category1` where the catalog has one (falls back to "Other").
  const items = await db
    .select({
      id: schema.orderItems.id,
      name: schema.orderItems.productName,
      totalCents: schema.orderItems.totalCents,
      categoryName: schema.productCategories.name,
    })
    .from(schema.orderItems)
    .leftJoin(
      schema.products,
      eq(schema.orderItems.productId, schema.products.id),
    )
    .leftJoin(
      schema.productCategories,
      eq(schema.products.categoryId, schema.productCategories.id),
    )
    .where(eq(schema.orderItems.orderId, order.id));

  const basketItems = items.length > 0
    ? items.map((it) => ({
        id: it.id,
        name: it.name,
        category1: it.categoryName ?? "Other",
        itemType: "PHYSICAL",
        price: (Number(it.totalCents) / 100).toFixed(2),
      }))
    : [
        // Defensive fallback: Iyzico requires >=1 basket item whose prices
        // sum to `price`. Shouldn't normally fire — checkout always inserts
        // order_items before calling createSession.
        {
          id: order.id,
          name: `Order ${order.orderNumber}`,
          category1: "Other",
          itemType: "PHYSICAL",
          price: (Number(order.subtotalCents) / 100).toFixed(2),
        },
      ];

  // Iyzico's invariant: sum(basketItems.price) == price. Recompute from
  // integer cents to avoid float drift when a discount or tax made
  // `subtotalCents` drift from the per-line totals.
  const basketSumCents = items.length > 0
    ? items.reduce((sum, it) => sum + Number(it.totalCents), 0)
    : Number(order.subtotalCents);

  // Iyzico accepts the all-ones placeholder for non-TR buyers; if the
  // storefront collected a real 11-digit TC kimlik into order.metadata it
  // gives materially better fraud/3DS outcomes, so prefer it.
  const orderMetadata = (order.metadata ?? {}) as Record<string, unknown>;
  const collectedIdentity = orderMetadata.identityNumber;
  const identityNumber =
    typeof collectedIdentity === "string" && /^\d{11}$/.test(collectedIdentity)
      ? collectedIdentity
      : "11111111111";

  const req: Record<string, unknown> = {
    locale: config.locale || "en",
    conversationId: order.id,
    price: (basketSumCents / 100).toFixed(2),
    paidPrice: (Number(order.totalCents) / 100).toFixed(2),
    currency: config.currency,
    basketId: order.orderNumber,
    paymentGroup: "PRODUCT",
    callbackUrl: `${siteUrl}/api/webhooks/iyzico`,
    buyer: {
      id: order.customerEmail,
      name: firstName,
      surname,
      identityNumber,
      email: order.customerEmail,
      // Real client IP materially improves Iyzico's fraud scoring. Falls
      // back to a loopback placeholder when the request couldn't resolve
      // one (e.g. direct connection, header stripped upstream).
      ip: ctx.clientIp || "127.0.0.1",
      registrationAddress: shippingAddress,
      city: shipping.city || "Not provided",
      country: shipping.country || "Turkey",
      zipCode: shipping.postal_code ?? "",
    },
    shippingAddress: {
      contactName: order.customerName ?? "",
      address: shippingAddress,
      city: shipping.city || "Not provided",
      country: shipping.country || "Turkey",
      zipCode: shipping.postal_code ?? "",
    },
    billingAddress: {
      contactName: order.customerName ?? "",
      address: billingAddress,
      city: billing.city || shipping.city || "Not provided",
      country: billing.country || shipping.country || "Turkey",
    },
    basketItems,
  };

  // Installments only exist for TRY; sending them for other currencies is
  // a no-op at Iyzico but clutters the request.
  if (config.currency === "TRY") {
    req.enabledInstallments = [1, 2, 3, 6, 9];
  }

  const result = await promisify<IyzipayInitResult>((cb) =>
    iyzipay.checkoutFormInitialize.create(req, cb),
  );
  if (result.status !== "success" || !result.paymentPageUrl) {
    throw new Error(
      `Iyzico init failed: ${result.errorMessage ?? "unknown error"}`,
    );
  }
  return { kind: "redirect", url: result.paymentPageUrl };
}

export async function handleWebhook(
  rawBody: string,
  _headers: Headers,
): Promise<WebhookEvent> {
  const iyzipay = await getClient();
  const params = new URLSearchParams(rawBody);
  const token = params.get("token");
  if (!token) throw new Error("iyzico webhook missing token");

  // The retrieve() call is our authentication: it signs the request with
  // our merchant API+secret keys, so Iyzico will only return a result for
  // a session we actually initiated. Foreign or forged tokens either 404
  // or come back with status != "success", which we reject below. This
  // is why we don't need (and Iyzico doesn't send) a request-body
  // signature header.
  const result = await promisify<IyzipayRetrieveResult>((cb) =>
    iyzipay.checkoutForm.retrieve({ locale: "en", token }, cb),
  );

  if (result.status !== "success") {
    throw new Error(
      `iyzico webhook retrieve failed: ${result.errorMessage ?? "unknown error"}`,
    );
  }

  const orderId = result.conversationId ?? "";
  if (!orderId) {
    throw new Error("iyzico webhook missing conversationId");
  }

  const paid = result.paymentStatus === "SUCCESS";
  return {
    orderId,
    paymentId: result.paymentId ?? token,
    status: paid ? "paid" : "failed",
    provider: "iyzico",
  };
}
