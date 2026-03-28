"use client";

import React, { Suspense, useEffect, useState } from "react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { CheckCircle, Package, Loader2 } from "lucide-react";
import { motion } from "framer-motion";
import { getOrder, confirmCheckout, formatPrice } from "@/lib/wemob-store";

export default function CheckoutSuccessPage() {
  return (
    <Suspense
      fallback={
        <div className="flex min-h-[50vh] flex-col items-center justify-center px-4">
          <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
          <p className="mt-4 text-muted-foreground">{{t.loadingOrderDetails}}</p>
        </div>
      }
    >
      <CheckoutSuccessContent />
    </Suspense>
  );
}

function CheckoutSuccessContent() {
  const searchParams = useSearchParams();
  const orderId = searchParams.get("order_id");
  const sessionId = searchParams.get("session_id");

  const [order, setOrder] = useState<any | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!orderId) {
      setLoading(false);
      setError("{{t.noOrderId}}");
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
        setError("{{t.orderNotFound}}");
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
        <p className="mt-4 text-muted-foreground">{{t.loadingOrderDetails}}</p>
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
          {{t.orderNotFoundDesc}}
        </p>
        <Link
          href="/products"
          className="mt-6 inline-flex h-10 items-center rounded-full bg-foreground px-6 text-sm font-medium text-background transition-opacity hover:opacity-90"
        >
          {{t.continueShopping}}
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
        {{t.orderConfirmed}}
      </h1>
      <p className="mt-2 text-muted-foreground">
        {{t.thankYouPurchase}}{" "}
        <span className="font-semibold text-foreground">{order.order_number || orderId}</span>.
      </p>

      <div className="mt-8 rounded-xl bg-muted/30 p-6 text-left">
        <h2 className="text-sm font-medium uppercase tracking-[0.15em] text-muted-foreground">
          {{t.orderSummary}}
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
                <p className="text-xs text-muted-foreground">{{t.qty}}: {item.quantity}</p>
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
