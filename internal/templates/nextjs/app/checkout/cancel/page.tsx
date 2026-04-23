import React from "react";
import Link from "next/link";
import { XCircle, MessageCircle } from "lucide-react";
import { getOrder } from "@/lib/wemob-store-server";

export default async function CheckoutCancelPage({
  searchParams,
}: {
  searchParams: Promise<{ order?: string; t?: string }>;
}) {
  const { order: orderId, t: viewToken } = await searchParams;
  // Order may not exist (user landed here directly) or the order row may have
  // been rolled back by the adapter failure path — both are fine. If we can
  // resolve one AND the caller carries the view token, show its order_number;
  // otherwise fall back to a generic message (also the intentional response
  // to anyone scraping order UUIDs).
  const order =
    orderId && viewToken ? await getOrder(orderId, viewToken) : null;

  return (
    <div className="mx-auto max-w-lg px-4 py-16 sm:px-6 sm:py-20">
      <div className="text-center">
        <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-muted/50">
          <XCircle className="h-8 w-8 text-muted-foreground" />
        </div>

        <h1 className="mt-6 text-2xl font-semibold tracking-tight text-foreground">
          {{t.paymentCancelledTitle}}
        </h1>
        <p className="mx-auto mt-2 max-w-md text-muted-foreground">
          {{t.paymentCancelled}}
        </p>
        {order && (
          <p className="mx-auto mt-3 max-w-md text-sm text-muted-foreground">
            <span className="font-semibold text-foreground">{order.order_number}</span> {{t.orderNotCompleted}}
          </p>
        )}

        <div className="mt-8 flex flex-col gap-3 sm:flex-row sm:justify-center">
          <Link
            href="/cart"
            className="inline-flex h-11 items-center justify-center rounded-full bg-foreground px-6 text-sm font-medium text-background transition-opacity hover:opacity-90"
          >
            {{t.backToCart}}
          </Link>
          <Link
            href="/products"
            className="inline-flex h-11 items-center justify-center rounded-full border border-border/60 px-6 text-sm font-medium text-foreground transition-colors hover:bg-muted"
          >
            {{t.continueShopping}}
          </Link>
        </div>
      </div>

      {/* Support link */}
      <div className="mt-8 text-center">
        <Link
          href="/contact"
          className="inline-flex items-center gap-1.5 text-sm text-muted-foreground underline underline-offset-4 transition-colors hover:text-foreground"
        >
          <MessageCircle className="h-3.5 w-3.5" />
          {{t.contactSupport}}
        </Link>
      </div>
    </div>
  );
}
