"use client";

import { FileText, DollarSign, CreditCard, Copyright, UserCheck, Scale } from "lucide-react";
import { motion } from "framer-motion";
import { useStoreConfig } from "@/lib/wemob-store";

export default function TermsOfServicePage() {
  const { config } = useStoreConfig();
  const storeName = config?.name || "Our Store";
  var toSlug = function(s: string) { return s.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/(^-|-$)/g, ""); };

  const sections = [
    { icon: FileText, title: "Acceptance of Terms", content: "By accessing and using the " + storeName + " website, you agree to be bound by these Terms of Service and all applicable laws and regulations. If you do not agree with any of these terms, you are prohibited from using this site." },
    { icon: DollarSign, title: "Products & Pricing", content: "All product descriptions, images, and prices are subject to change without notice. We reserve the right to modify or discontinue any product at any time. Prices are displayed in the store\u2019s configured currency and include applicable taxes unless otherwise stated. We make every effort to ensure pricing accuracy, but errors may occur. In the event of a pricing error, we reserve the right to cancel or refuse orders placed at the incorrect price." },
    { icon: CreditCard, title: "Orders & Payment", content: "By placing an order, you represent that the information provided is accurate and that you are authorized to use the payment method. We reserve the right to refuse or cancel any order for reasons including product availability, errors in pricing or product information, or suspected fraudulent activity." },
    { icon: Copyright, title: "Intellectual Property", content: "All content on this website, including text, graphics, logos, images, and software, is the property of " + storeName + " or its content suppliers and is protected by intellectual property laws. You may not reproduce, distribute, modify, or create derivative works from any content without our express written permission." },
    { icon: UserCheck, title: "User Accounts", content: "If you create an account, you are responsible for maintaining the confidentiality of your account credentials and for all activities under your account. You agree to notify us immediately of any unauthorized use of your account. We reserve the right to terminate accounts that violate these terms." },
    { icon: Scale, title: "Limitation of Liability", content: storeName + " shall not be liable for any indirect, incidental, special, consequential, or punitive damages resulting from your use of or inability to use the website or products. Our total liability for any claim arising from these terms shall not exceed the amount you paid for the product giving rise to the claim." },
    { icon: FileText, title: "Governing Law", content: "These Terms of Service shall be governed by and construed in accordance with applicable local laws, without regard to conflict of law principles." },
    { icon: FileText, title: "Changes to Terms", content: "We reserve the right to update these Terms of Service at any time. Changes will be effective immediately upon posting. Your continued use of the website after changes constitutes acceptance of the updated terms." },
  ];

  return (
    <div className="mx-auto max-w-3xl px-4 py-12 sm:px-6 sm:py-16 lg:px-8">
      <h1 className="text-3xl font-semibold tracking-tight text-foreground sm:text-4xl">{{t.termsPolicyTitle}}</h1>
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
            <p className="text-sm leading-relaxed text-muted-foreground">{section.content}</p>
          </motion.section>
        ))}

        <div className="mt-4 rounded-2xl border border-border/40 p-6 text-center">
          <p className="text-sm text-muted-foreground">Have questions about our terms of service?</p>
          <a href="/contact" className="mt-2 inline-flex items-center text-sm font-medium text-foreground underline underline-offset-4 transition-colors hover:text-foreground/80">Contact Us</a>
        </div>
      </div>
    </div>
  );
}
