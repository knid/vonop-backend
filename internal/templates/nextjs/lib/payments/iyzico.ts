import "server-only";
// lib/payments/iyzico.ts — Iyzico Checkout Form adapter.
// This file is template-managed. Do not modify.
//
// Env vars (mapped by backend integrations.go):
//   IYZICO_API_KEY     — merchant API key
//   IYZICO_SECRET_KEY  — merchant secret key
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

import type { Order, Store } from "@/lib/schema";

import type { CheckoutResult, WebhookEvent } from "./index";

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
  return new Iyzipay({ apiKey, secretKey, uri: "https://api.iyzipay.com" });
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
  store: Store,
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

  const req: Record<string, unknown> = {
    locale: store.locale || "en",
    conversationId: order.id,
    price: (Number(order.subtotalCents) / 100).toFixed(2),
    paidPrice: (Number(order.totalCents) / 100).toFixed(2),
    currency: store.currency,
    basketId: order.orderNumber,
    paymentGroup: "PRODUCT",
    callbackUrl: `${siteUrl}/api/webhooks/iyzico`,
    enabledInstallments: [1, 2, 3, 6, 9],
    buyer: {
      id: order.customerEmail,
      name: firstName,
      surname,
      // Iyzico requires this field; a placeholder is used for non-TR buyers.
      identityNumber: "11111111111",
      email: order.customerEmail,
      ip: "127.0.0.1",
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
    // Minimal single basket-item representing the order total. For richer
    // line-item breakdown, read order_items from the tenant DB.
    basketItems: [
      {
        id: order.id,
        name: `Order ${order.orderNumber}`,
        category1: "Other",
        itemType: "PHYSICAL",
        price: (Number(order.subtotalCents) / 100).toFixed(2),
      },
    ],
  };

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

  const result = await promisify<IyzipayRetrieveResult>((cb) =>
    iyzipay.checkoutForm.retrieve({ locale: "en", token }, cb),
  );

  const orderId = result.conversationId ?? "";
  const paid = result.paymentStatus === "SUCCESS";
  return {
    orderId,
    paymentId: result.paymentId ?? token,
    status: paid ? "paid" : "failed",
    provider: "iyzico",
  };
}
