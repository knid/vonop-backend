"use client";

import React, { useState } from "react";
import Link from "next/link";
import { XCircle, ChevronDown, MessageCircle } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";

export default function CheckoutCancelPage() {
  var faqs = [
    { q: "Why was my payment cancelled?", a: "Payments can be cancelled if you close the payment window, if there are insufficient funds, or if your bank declined the transaction. No charges have been made to your account." },
    { q: "{{t.paymentChargedQuestion}}", a: "No. If a payment is cancelled, no charges are applied. If you see a pending charge, it will be automatically reversed within 3\u20135 business days." },
    { q: "Can I use a different payment method?", a: "Yes. When you try again, you can select a different payment method at checkout." },
    { q: "{{t.cartSavedQuestion}}", a: "{{t.cartSavedAnswer}}" },
  ];

  var [openFaq, setOpenFaq] = useState<number | null>(null);

  return (
    <div className="mx-auto max-w-lg px-4 py-16 sm:px-6 sm:py-20">
      <div className="text-center">
        <motion.div
          initial={{ scale: 0 }}
          animate={{ scale: 1 }}
          transition={{ type: "spring", stiffness: 200, damping: 15 }}
          className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-muted/50"
        >
          <XCircle className="h-8 w-8 text-muted-foreground" />
        </motion.div>

        <h1 className="mt-6 text-2xl font-semibold tracking-tight text-foreground">
          Payment Cancelled
        </h1>
        <p className="mx-auto mt-2 max-w-md text-muted-foreground">
          {{t.paymentCancelled}}
        </p>

        <div className="mt-8 flex flex-col gap-3 sm:flex-row sm:justify-center">
          <Link
            href="/checkout"
            className="inline-flex h-11 items-center justify-center rounded-full bg-foreground px-6 text-sm font-medium text-background transition-opacity hover:opacity-90"
          >
            Try Again
          </Link>
          <Link
            href="/products"
            className="inline-flex h-11 items-center justify-center rounded-full border border-border/60 px-6 text-sm font-medium text-foreground transition-colors hover:bg-muted"
          >
            {{t.continueShopping}}
          </Link>
        </div>
      </div>

      {/* FAQ Section */}
      <div className="mt-12 border-t border-border/40 pt-10">
        <h2 className="mb-5 text-center text-sm font-medium uppercase tracking-[0.15em] text-muted-foreground">
          {{t.commonQuestions}}
        </h2>
        <div className="space-y-2">
          {faqs.map(function(faq, i) {
            return (
              <div key={i} className="rounded-xl border border-border/40 bg-muted/10">
                <button
                  onClick={function() { setOpenFaq(openFaq === i ? null : i); }}
                  className="flex w-full items-center justify-between px-5 py-4 text-left text-sm font-medium text-foreground"
                >
                  {faq.q}
                  <ChevronDown className={"h-4 w-4 flex-shrink-0 text-muted-foreground transition-transform duration-200 " + (openFaq === i ? "rotate-180" : "")} />
                </button>
                <AnimatePresence>
                  {openFaq === i && (
                    <motion.div
                      initial={{ height: 0, opacity: 0 }}
                      animate={{ height: "auto", opacity: 1 }}
                      exit={{ height: 0, opacity: 0 }}
                      transition={{ duration: 0.2 }}
                      className="overflow-hidden"
                    >
                      <p className="px-5 pb-4 text-sm leading-relaxed text-muted-foreground">
                        {faq.a}
                      </p>
                    </motion.div>
                  )}
                </AnimatePresence>
              </div>
            );
          })}
        </div>
      </div>

      {/* Support link */}
      <div className="mt-8 text-center">
        <Link
          href="/contact"
          className="inline-flex items-center gap-1.5 text-sm text-muted-foreground underline underline-offset-4 transition-colors hover:text-foreground"
        >
          <MessageCircle className="h-3.5 w-3.5" />
          Contact Support
        </Link>
      </div>
    </div>
  );
}
