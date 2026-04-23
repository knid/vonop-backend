import React from "react";
import Link from "next/link";
import { notFound } from "next/navigation";
import { CheckCircle, Package } from "lucide-react";
import { getOrder } from "@/lib/wemob-store-server";
import { formatPrice } from "@/lib/wemob-store";

export default async function CheckoutSuccessPage({
  searchParams,
}: {
  searchParams: Promise<{ order?: string }>;
}) {
  const { order: orderId } = await searchParams;
  if (!orderId) notFound();

  const order = await getOrder(orderId);
  if (!order) notFound();

  return (
    <div className="mx-auto max-w-lg px-4 py-16 text-center sm:px-6 sm:py-20">
      <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-green-100 dark:bg-green-900/30">
        <CheckCircle className="h-8 w-8 text-green-600 dark:text-green-400" />
      </div>

      <h1 className="mt-6 text-3xl font-semibold tracking-tight text-foreground">
        {{t.orderConfirmed}}
      </h1>
      <p className="mt-2 text-muted-foreground">
        {{t.thankYouPurchase}}{" "}
        <span className="font-semibold text-foreground">{order.order_number}</span>.
      </p>

      <div className="mt-8 rounded-xl bg-muted/30 p-6 text-left">
        <h2 className="text-sm font-medium uppercase tracking-[0.15em] text-muted-foreground">
          {{t.orderSummary}}
        </h2>
        <ul className="mt-4 divide-y divide-border/40">
          {order.items.map((item, idx) => (
            <li
              key={idx}
              className="flex items-center justify-between py-3"
            >
              <div>
                <p className="text-sm font-medium text-foreground">{item.product_name}</p>
                <p className="text-xs text-muted-foreground">{{t.qty}}: {item.quantity}</p>
              </div>
              <p className="text-sm font-medium text-foreground">
                {formatPrice(item.total_cents, order.currency)}
              </p>
            </li>
          ))}
        </ul>
        <div className="mt-4 flex items-center justify-between border-t border-border/40 pt-4">
          <span className="font-semibold text-foreground">Total</span>
          <span className="text-lg font-semibold tracking-tight text-foreground">
            {formatPrice(order.total_cents, order.currency)}
          </span>
        </div>
      </div>

      {/* {{t.whatHappensNext}} */}
      <div className="mt-10 rounded-xl bg-muted/20 p-6 text-left">
        <h3 className="mb-4 text-sm font-medium uppercase tracking-[0.15em] text-muted-foreground">{{t.whatHappensNext}}</h3>
        <div className="space-y-4">
          {[
            { step: "1", title: "{{t.stepOrderConfirmed}}", desc: "{{t.stepOrderConfirmedDesc}}" },
            { step: "2", title: "{{t.stepProcessing}}", desc: "{{t.stepProcessingDesc}}" },
            { step: "3", title: "{{t.stepShipped}}", desc: "{{t.stepShippedDesc}}" },
            { step: "4", title: "{{t.stepDelivered}}", desc: "{{t.stepDeliveredDesc}}" },
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
          {{t.continueShopping}}
        </Link>
        <Link href="/contact" className="text-sm text-muted-foreground underline underline-offset-4 transition-colors hover:text-foreground">
          {{t.needHelpOrder}}
        </Link>
      </div>
    </div>
  );
}
