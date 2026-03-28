"use client";

import { Shield, Database, Cookie, Share2, Lock, User } from "lucide-react";
import { motion } from "framer-motion";
import { useStoreConfig } from "@/lib/wemob-store";

export default function PrivacyPolicyPage() {
  const { config } = useStoreConfig();
  const storeName = config?.name || "Our Store";
  var toSlug = function(s: string) { return s.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/(^-|-$)/g, ""); };

  const sections = [
    { icon: Shield, title: "Introduction", content: storeName + " (\u201Cwe\u201D, \u201Cour\u201D, or \u201Cus\u201D) is committed to protecting your privacy. This Privacy Policy explains how we collect, use, disclose, and safeguard your information when you visit our website and make purchases from our store." },
    { icon: Database, title: "Information We Collect", content: "We may collect information about you in a variety of ways, including:", items: ["Personal Data: Name, email address, shipping address, phone number, and payment information provided when you place an order.", "Order Data: Products purchased, order history, and transaction details.", "Usage Data: Browser type, IP address, pages visited, and time spent on our website, collected automatically through cookies and similar technologies.", "Communication Data: Information you provide when contacting our support team or subscribing to our newsletter."] },
    { icon: User, title: "How We Use Your Information", items: ["Process and fulfill your orders, including shipping and payment processing.", "Send order confirmations, shipping updates, and receipts.", "Respond to your inquiries and provide customer support.", "Send promotional emails and newsletters (with your consent).", "Improve our website, products, and services.", "Prevent fraudulent transactions and protect against unauthorized activity."] },
    { icon: Cookie, title: "Cookies", content: "We use cookies and similar tracking technologies to enhance your browsing experience, analyze site traffic, and personalize content. You can control cookie preferences through your browser settings. Disabling cookies may affect some website functionality." },
    { icon: Share2, title: "Data Sharing", content: "We do not sell your personal information. We may share your data with trusted third-party service providers who assist us in operating our website, processing payments, and delivering orders. These providers are obligated to protect your information and use it only for the purposes we specify." },
    { icon: Lock, title: "Data Security", content: "We implement industry-standard security measures to protect your personal information, including SSL encryption for all data transmission. However, no method of internet transmission is 100% secure, and we cannot guarantee absolute security." },
    { icon: User, title: "Your Rights", content: "Depending on your location, you may have the right to access, correct, delete, or port your personal data. You may also opt out of marketing communications at any time by clicking the unsubscribe link in our emails or contacting us directly." },
    { icon: Shield, title: "Contact Us", content: "If you have questions about this Privacy Policy or our data practices, please contact us through our contact page." },
  ];

  return (
    <div className="mx-auto max-w-3xl px-4 py-12 sm:px-6 sm:py-16 lg:px-8">
      <h1 className="text-3xl font-semibold tracking-tight text-foreground sm:text-4xl">{{t.privacyPolicyTitle}}</h1>
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
            key={section.title + idx}
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
              <div className={"space-y-2 " + (section.content ? "mt-3" : "")}>
                {section.items.map((item, i) => (
                  <div key={i} className="flex items-start gap-3 text-sm leading-relaxed text-muted-foreground">
                    <span className="mt-1 h-1.5 w-1.5 flex-shrink-0 rounded-full bg-muted-foreground/40" />
                    <span>{item}</span>
                  </div>
                ))}
              </div>
            )}
          </motion.section>
        ))}

        <div className="mt-4 rounded-2xl border border-border/40 p-6 text-center">
          <p className="text-sm text-muted-foreground">Have questions about your privacy or data?</p>
          <a href="/contact" className="mt-2 inline-flex items-center text-sm font-medium text-foreground underline underline-offset-4 transition-colors hover:text-foreground/80">Contact Us</a>
        </div>
      </div>
    </div>
  );
}
