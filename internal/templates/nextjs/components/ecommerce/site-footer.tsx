"use client";

import React, { useState } from "react";
import Link from "next/link";
import { ArrowUp } from "lucide-react";
import { useCategories, useStoreConfig } from "@/lib/wemob-store";

export function SiteFooter() {
  const { categories } = useCategories();
  const { config: storeConfig } = useStoreConfig();
  const [email, setEmail] = useState("");
  const [subscribed, setSubscribed] = useState(false);

  const allCategories = categories ?? [];
  const shopLinks = [
    { label: "{{t.allProducts}}", href: "/products" },
    ...allCategories.slice(0, 6).map((cat) => ({
      label: cat.name,
      href: "/categories/" + cat.slug,
    })),
  ];

  const companyLinks = [
    { label: "{{t.about}}", href: "/about" },
    { label: "{{t.contact}}", href: "/contact" },
  ];

  const supportLinks = [
    { label: "{{t.faq}}", href: "/faq" },
    { label: "{{t.shippingPolicy}}", href: "/policies/shipping" },
    { label: "{{t.returns}}", href: "/policies/returns" },
  ];

  const legalLinks = [
    { label: "{{t.privacyPolicy}}", href: "/policies/privacy" },
    { label: "{{t.termsOfService}}", href: "/policies/terms" },
  ];

  return (
    <footer className="border-t border-border/40 bg-background">
      <div className="mx-auto max-w-7xl px-4 py-16 sm:px-6 lg:px-8 lg:py-20">
        <div className="grid grid-cols-1 gap-10 sm:grid-cols-2 lg:grid-cols-4">
          <div className="sm:col-span-2 lg:col-span-1">
            <Link href="/" className="text-lg font-bold tracking-tight text-foreground">
              {storeConfig?.name}
            </Link>
            <p className="mt-3 text-sm leading-relaxed text-muted-foreground">
              {{t.footerDescription}}
              seamless shopping experience.
            </p>
            {/* Social icons */}
            <div className="mt-6 flex gap-3">
              <a
                href="#"
                className="flex h-9 w-9 items-center justify-center rounded-full border border-border/40 text-muted-foreground transition-colors hover:border-foreground hover:text-foreground"
                aria-label="Instagram"
              >
                <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <rect x="2" y="2" width="20" height="20" rx="5" />
                  <circle cx="12" cy="12" r="5" />
                  <circle cx="17.5" cy="6.5" r="1.5" fill="currentColor" stroke="none" />
                </svg>
              </a>
              <a
                href="#"
                className="flex h-9 w-9 items-center justify-center rounded-full border border-border/40 text-muted-foreground transition-colors hover:border-foreground hover:text-foreground"
                aria-label="Twitter"
              >
                <svg className="h-4 w-4" viewBox="0 0 24 24" fill="currentColor">
                  <path d="M18.244 2.25h3.308l-7.227 8.26 8.502 11.24H16.17l-5.214-6.817L4.99 21.75H1.68l7.73-8.835L1.254 2.25H8.08l4.713 6.231zm-1.161 17.52h1.833L7.084 4.126H5.117z" />
                </svg>
              </a>
              <a
                href="#"
                className="flex h-9 w-9 items-center justify-center rounded-full border border-border/40 text-muted-foreground transition-colors hover:border-foreground hover:text-foreground"
                aria-label="Facebook"
              >
                <svg className="h-4 w-4" viewBox="0 0 24 24" fill="currentColor">
                  <path d="M9.101 23.691v-7.98H6.627v-3.667h2.474v-1.58c0-4.085 1.848-5.978 5.858-5.978.401 0 1.09.044 1.613.115v3.146c-.427-.044-.72-.065-.95-.065-1.35 0-1.872.513-1.872 1.846v2.516h3.332l-.468 3.667h-2.864v8.245C19.396 23.238 24 18.179 24 12.044c0-6.627-5.373-12-12-12s-12 5.373-12 12c0 5.628 3.874 10.35 9.101 11.647z" />
                </svg>
              </a>
            </div>

            {/* Newsletter */}
            <div className="mt-8">
              <h3 className="text-[11px] font-medium uppercase tracking-[0.2em] text-muted-foreground/50">Newsletter</h3>
              <p className="mt-3 text-xs text-muted-foreground">{{t.newsletterDesc}}</p>
              {subscribed ? (
                <p className="mt-3 text-xs font-medium text-green-600 dark:text-green-400">Thank you for subscribing!</p>
              ) : (
                <form onSubmit={(e) => { e.preventDefault(); if (email.trim()) setSubscribed(true); }} className="mt-3 flex gap-2">
                  <input
                    type="email"
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    placeholder="your@email.com"
                    required
                    className="h-9 flex-1 rounded-full border border-border/60 bg-background px-4 text-xs text-foreground placeholder:text-muted-foreground focus:border-foreground focus:outline-none"
                  />
                  <button
                    type="submit"
                    className="h-9 rounded-full bg-foreground px-4 text-xs font-medium text-background transition-opacity hover:opacity-90"
                  >
                    {{t.subscribe}}
                  </button>
                </form>
              )}
            </div>
          </div>

          <div>
            <h3 className="text-[11px] font-medium uppercase tracking-[0.2em] text-muted-foreground/50">
              {{t.shop}}
            </h3>
            <ul className="mt-4 space-y-2.5">
              {shopLinks.map((link, i) => (
                <li key={link.href + "-" + i}>
                  <Link
                    href={link.href}
                    className="text-sm text-muted-foreground transition-colors hover:text-foreground"
                  >
                    {link.label}
                  </Link>
                </li>
              ))}
              {allCategories.length > 6 && (
                <li>
                  <Link
                    href="/products"
                    className="text-sm font-medium text-muted-foreground transition-colors hover:text-foreground"
                  >
                    {"{{t.viewAll}}"}
                  </Link>
                </li>
              )}
            </ul>
          </div>

          <div>
            <h3 className="text-[11px] font-medium uppercase tracking-[0.2em] text-muted-foreground/50">
              {{t.company}}
            </h3>
            <ul className="mt-4 space-y-2.5">
              {companyLinks.map((link) => (
                <li key={link.href}>
                  <Link
                    href={link.href}
                    className="text-sm text-muted-foreground transition-colors hover:text-foreground"
                  >
                    {link.label}
                  </Link>
                </li>
              ))}
            </ul>

            <h3 className="mt-8 text-[11px] font-medium uppercase tracking-[0.2em] text-muted-foreground/50">
              {{t.support}}
            </h3>
            <ul className="mt-4 space-y-2.5">
              {supportLinks.map((link) => (
                <li key={link.href}>
                  <Link
                    href={link.href}
                    className="text-sm text-muted-foreground transition-colors hover:text-foreground"
                  >
                    {link.label}
                  </Link>
                </li>
              ))}
            </ul>
          </div>

          <div>
            <h3 className="text-[11px] font-medium uppercase tracking-[0.2em] text-muted-foreground/50">
              {{t.legal}}
            </h3>
            <ul className="mt-4 space-y-2.5">
              {legalLinks.map((link) => (
                <li key={link.href}>
                  <Link
                    href={link.href}
                    className="text-sm text-muted-foreground transition-colors hover:text-foreground"
                  >
                    {link.label}
                  </Link>
                </li>
              ))}
            </ul>
          </div>
        </div>

        <div className="mt-16 border-t border-border/40 pt-8">
          <div className="flex flex-col items-center gap-6 sm:flex-row sm:justify-between">
            <p className="text-xs text-muted-foreground/60">
              &copy; {new Date().getFullYear()} {storeConfig?.name}. {{t.allRightsReserved}}
            </p>

            {/* Payment method icons */}
            <div className="flex items-center gap-3">
              <span className="mr-1 text-[10px] uppercase tracking-wider text-muted-foreground/40">{{t.weAccept}}</span>
              <svg className="h-6 w-auto text-muted-foreground/50" viewBox="0 0 48 32" fill="none"><rect width="48" height="32" rx="4" fill="currentColor" opacity="0.1"/><path d="M19.5 20.5l2-9h3l-2 9h-3zm13-9l-3 6.2-.4-2-1-5.4s-.1-.8-1.2-.8h-4.7l-.1.3s1.4.3 3 1.3l2.3 8.4h3.1l4.7-8h-2.7zm-19 0l-3 9h-3.2l-1.8-7.2c-.1-.4-.2-.6-.5-.7-.6-.3-1.5-.6-2.3-.8l.1-.3h4.8c.6 0 1.2.4 1.3 1.2l1.2 6.2 2.9-7.4h3.5zm26.2 6.2c0-2.5-3.4-2.6-3.4-3.7 0-.3.3-.7 1.1-.8.4 0 1.5-.1 2.7.5l.5-2.2c-.7-.2-1.5-.5-2.6-.5-2.7 0-4.7 1.5-4.7 3.6 0 1.6 1.4 2.4 2.4 3 1.1.5 1.4.8 1.4 1.3 0 .7-.8 1-1.6 1-.7 0-1.9-.3-2.5-.5l-.5 2.2c.6.3 1.6.5 2.7.5 2.9 0 4.8-1.4 4.8-3.7l-.3.3z" fill="currentColor" opacity="0.5"/></svg>
              <svg className="h-6 w-auto text-muted-foreground/50" viewBox="0 0 48 32" fill="none"><rect width="48" height="32" rx="4" fill="currentColor" opacity="0.1"/><circle cx="19" cy="16" r="8" fill="currentColor" opacity="0.2"/><circle cx="29" cy="16" r="8" fill="currentColor" opacity="0.3"/></svg>
              <svg className="h-6 w-auto text-muted-foreground/50" viewBox="0 0 48 32" fill="none"><rect width="48" height="32" rx="4" fill="currentColor" opacity="0.1"/><text x="24" y="20" textAnchor="middle" fontSize="10" fontWeight="bold" fill="currentColor" opacity="0.4">AMEX</text></svg>
              <svg className="h-6 w-auto text-muted-foreground/50" viewBox="0 0 48 32" fill="none"><rect width="48" height="32" rx="4" fill="currentColor" opacity="0.1"/><text x="24" y="19" textAnchor="middle" fontSize="8" fontWeight="600" fontStyle="italic" fill="currentColor" opacity="0.4">PayPal</text></svg>
            </div>
          </div>

          <p className="mt-6 text-center text-[11px] text-muted-foreground/40">
            {{t.secureShoppingFooter}}
          </p>

          {/* Back to top */}
          <div className="mt-4 flex justify-center">
            <button
              onClick={() => window.scrollTo({ top: 0, behavior: "smooth" })}
              className="flex items-center gap-1.5 text-xs text-muted-foreground/50 transition-colors hover:text-muted-foreground"
            >
              <ArrowUp className="h-3.5 w-3.5" />
              {{t.backToTop}}
            </button>
          </div>
        </div>
      </div>
    </footer>
  );
}
