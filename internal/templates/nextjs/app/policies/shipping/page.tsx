"use client";

import { Truck, Clock, Globe, MapPin, Package } from "lucide-react";
import { motion } from "framer-motion";
import { useStoreConfig } from "@/lib/wemob-store";

export default function ShippingPolicyPage() {
  const { config } = useStoreConfig();
  const storeName = config?.name || "Our Store";
  var toSlug = function(s: string) { return s.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/(^-|-$)/g, ""); };

  const sections = [
    { icon: Clock, title: "{{t.processingTime}}", content: "All orders placed with " + storeName + " are processed within 1\u20133 business days. Orders placed on weekends or holidays will be processed on the next business day. You will receive a confirmation email with tracking information once your order has shipped." },
    { icon: Globe, title: "{{t.internationalShipping}}", content: "We currently ship to select international destinations. International shipping rates and delivery times vary by location and will be calculated at checkout. Please note that international orders may be subject to customs duties and taxes, which are the responsibility of the recipient." },
    { icon: MapPin, title: "{{t.orderTracking}}", content: "Once your order ships, you will receive an email with a tracking number. You can use this number to track your package on the carrier\u2019s website. If you have any questions about your shipment, please don\u2019t hesitate to contact our support team." },
    { icon: Package, title: "{{t.shippingRestrictions}}", content: "Some items may have shipping restrictions due to size, weight, or destination regulations. If an item cannot be shipped to your address, you will be notified during checkout. We are unable to ship to P.O. boxes for express and overnight orders." },
  ];

  return (
    <div className="mx-auto max-w-3xl px-4 py-12 sm:px-6 sm:py-16 lg:px-8">
      <h1 className="text-3xl font-semibold tracking-tight text-foreground sm:text-4xl">{{t.shippingPolicyTitle}}</h1>
      <div className="mt-3">
        <span className="inline-flex rounded-full bg-muted/30 px-3 py-1 text-xs text-muted-foreground">
          Last updated: {new Date().toLocaleDateString("en-US", { month: "long", year: "numeric" })}
        </span>
      </div>

      <nav className="mt-8 flex flex-wrap gap-2" aria-label="Quick links">
        <a href="#shipping-rates-delivery-times" className="rounded-full border border-border/40 px-3 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-muted/30 hover:text-foreground">{{t.shippingRates}}</a>
        {sections.map((s) => (
          <a key={s.title} href={"#" + toSlug(s.title)} className="rounded-full border border-border/40 px-3 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-muted/30 hover:text-foreground">{s.title}</a>
        ))}
      </nav>

      <div className="mt-10 space-y-6">
        {/* Shipping rates table */}
        <motion.div
          id="shipping-rates-delivery-times"
          initial={{ opacity: 0, y: 20 }}
          whileInView={{ opacity: 1, y: 0 }}
          viewport={{ once: true }}
          className="rounded-2xl bg-muted/20 p-6 sm:p-8"
        >
          <div className="mb-4 flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-muted/50">
              <Truck className="h-5 w-5 text-muted-foreground" />
            </div>
            <h2 className="text-lg font-semibold tracking-tight text-foreground">{{t.shippingRatesAndDelivery}}</h2>
          </div>
          <div className="overflow-hidden rounded-xl border border-border/40">
            <table className="w-full text-left text-sm">
              <thead className="bg-muted/30">
                <tr>
                  <th className="px-4 py-3 text-[11px] font-medium uppercase tracking-[0.15em] text-muted-foreground">Method</th>
                  <th className="px-4 py-3 text-[11px] font-medium uppercase tracking-[0.15em] text-muted-foreground">Estimated Delivery</th>
                  <th className="px-4 py-3 text-[11px] font-medium uppercase tracking-[0.15em] text-muted-foreground">Cost</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border/40 text-muted-foreground">
                <tr><td className="px-4 py-3">{{t.standardShipping}}</td><td className="px-4 py-3">{{t.standardShippingTime}}</td><td className="px-4 py-3">{{t.standardShippingCost}}</td></tr>
                <tr><td className="px-4 py-3">{{t.expressShipping}}</td><td className="px-4 py-3">{{t.expressShippingTime}}</td><td className="px-4 py-3">{{t.expressShippingCost}}</td></tr>
                <tr><td className="px-4 py-3">{{t.overnightShipping}}</td><td className="px-4 py-3">{{t.overnightShippingTime}}</td><td className="px-4 py-3">{{t.overnightShippingCost}}</td></tr>
              </tbody>
            </table>
          </div>
        </motion.div>

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
            <p className="text-sm leading-relaxed text-muted-foreground">{section.content}</p>
          </motion.section>
        ))}

        <div className="mt-4 rounded-2xl border border-border/40 p-6 text-center">
          <p className="text-sm text-muted-foreground">Have questions about our shipping policy?</p>
          <a href="/contact" className="mt-2 inline-flex items-center text-sm font-medium text-foreground underline underline-offset-4 transition-colors hover:text-foreground/80">Contact Us</a>
        </div>
      </div>
    </div>
  );
}
