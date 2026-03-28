"use client";

import { RotateCcw, CheckCircle, Package, CreditCard, AlertCircle } from "lucide-react";
import { motion } from "framer-motion";
import { useStoreConfig } from "@/lib/wemob-store";

export default function ReturnsPolicyPage() {
  const { config } = useStoreConfig();
  const storeName = config?.name || "Our Store";
  var toSlug = function(s: string) { return s.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/(^-|-$)/g, ""); };

  const sections = [
    { icon: RotateCcw, title: "Our Return Promise", content: "At " + storeName + ", we want you to be completely satisfied with your purchase. If for any reason you are not happy with your order, we offer a hassle-free return and exchange policy within 30 days of delivery." },
    { icon: CheckCircle, title: "Eligibility", items: ["Items must be returned within 30 days of delivery.", "Products must be in their original condition, unworn, unwashed, and with all tags attached.", "Items must be in their original packaging when possible.", "Sale items and gift cards are final sale and cannot be returned.", "Personalized or custom-made items are non-returnable."] },
    { icon: Package, title: "How to Return", items: ["Contact our support team to initiate a return request.", "You will receive a return authorization and prepaid shipping label via email.", "Pack the item securely in its original packaging.", "Attach the shipping label and drop off the package at the designated carrier.", "Once we receive and inspect the item, your refund will be processed within {{t.standardShippingTime}}."], numbered: true },
    { icon: RotateCcw, title: "Exchanges", content: "If you would like to exchange an item for a different size or color, please follow the return process above and place a new order for the desired item. This ensures the fastest processing time." },
    { icon: CreditCard, title: "Refunds", content: "Refunds will be issued to the original payment method. Please allow {{t.standardShippingTime}} for the refund to appear on your statement after we have received and processed your return. Shipping costs are non-refundable unless the return is due to a defective or incorrect item." },
    { icon: AlertCircle, title: "Damaged or Defective Items", content: "If you received a damaged or defective item, please contact us within 48 hours of delivery with photos of the issue. We will arrange a replacement or full refund at no additional cost to you." },
  ];

  return (
    <div className="mx-auto max-w-3xl px-4 py-12 sm:px-6 sm:py-16 lg:px-8">
      <h1 className="text-3xl font-semibold tracking-tight text-foreground sm:text-4xl">Return & Exchange Policy</h1>
      <div className="mt-3">
        <span className="inline-flex rounded-full bg-muted/30 px-3 py-1 text-xs text-muted-foreground">
          Last updated: {new Date().toLocaleDateString("en-US", { month: "long", year: "numeric" })}
        </span>
      </div>

      <nav className="mt-8 flex flex-wrap gap-2" aria-label="Quick links">
        {sections.map((s) => (
          <a key={s.title} href={"#" + toSlug(s.title)} className="rounded-full border border-border/40 px-3 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-muted/30 hover:text-foreground">{s.title}</a>
        ))}
      </nav>

      <div className="mt-10 space-y-6">
        {sections.map((section, idx) => (
          <motion.section
            key={section.title}
            id={toSlug(section.title)}
            initial={{ opacity: 0, y: 20 }}
            whileInView={{ opacity: 1, y: 0 }}
            viewport={{ once: true }}
            transition={{ delay: idx * 0.1 }}
            className="rounded-2xl bg-muted/20 p-6 sm:p-8"
          >
            <div className="mb-4 flex items-center gap-3">
              <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-muted/50">
                <section.icon className="h-5 w-5 text-muted-foreground" />
              </div>
              <h2 className="text-lg font-semibold tracking-tight text-foreground">{section.title}</h2>
            </div>
            {section.content && <p className="text-sm leading-relaxed text-muted-foreground">{section.content}</p>}
            {section.items && (
              <div className="space-y-2">
                {section.items.map((item, i) => (
                  <div key={i} className="flex items-start gap-3 text-sm leading-relaxed text-muted-foreground">
                    <span className="mt-0.5 flex h-5 w-5 flex-shrink-0 items-center justify-center rounded-full bg-muted/50 text-[11px] font-medium text-muted-foreground">
                      {section.numbered ? i + 1 : "\u2022"}
                    </span>
                    <span>{item}</span>
                  </div>
                ))}
              </div>
            )}
          </motion.section>
        ))}

        <div className="mt-4 rounded-2xl border border-border/40 p-6 text-center">
          <p className="text-sm text-muted-foreground">Need to start a return?</p>
          <a href="/contact" className="mt-2 inline-flex items-center text-sm font-medium text-foreground underline underline-offset-4 transition-colors hover:text-foreground/80">Contact Our Support Team</a>
        </div>
      </div>
    </div>
  );
}
