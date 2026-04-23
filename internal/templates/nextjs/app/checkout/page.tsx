"use client";

import React from "react";
import Link from "next/link";
import { ArrowLeft, Check, Lock, Shield } from "lucide-react";
import { CheckoutForm } from "@/components/ecommerce/checkout-form";

export default function CheckoutPage() {
  const steps = ["{{t.stepCart}}", "{{t.stepInformation}}", "{{t.stepPayment}}"];

  return (
    <div className="mx-auto max-w-4xl px-4 py-12 sm:px-6 sm:py-16 lg:px-8">
      {/* Security trust bar */}
      <div className="mb-8 flex items-center justify-center gap-2 rounded-xl bg-muted/20 px-4 py-3 text-center text-xs text-muted-foreground">
        <Lock className="h-3.5 w-3.5 flex-shrink-0" />
        <span>{{t.secureCheckoutBanner}}</span>
      </div>

      {/* Step progress with connecting lines and checkmarks */}
      <div className="mb-10">
        <div className="flex items-center justify-center">
          {steps.map((step, i) => {
            const isCompleted = i < 1;
            const isCurrent = i === 1;
            return (
              <React.Fragment key={step}>
                {i > 0 && (
                  <div className={"mx-1 h-0.5 w-10 rounded-full sm:mx-2 sm:w-16 " + (isCompleted ? "bg-foreground" : "bg-border/40")} />
                )}
                <div className="flex items-center gap-2">
                  <div className={"flex h-8 w-8 items-center justify-center rounded-full text-xs font-semibold transition-colors " +
                    (isCompleted ? "bg-foreground text-background" : isCurrent ? "bg-foreground text-background ring-4 ring-foreground/10" : "border-2 border-border/40 text-muted-foreground/50")}>
                    {isCompleted ? <Check className="h-4 w-4" /> : i + 1}
                  </div>
                  <span className={"hidden text-sm sm:block " + (isCurrent ? "font-semibold text-foreground" : isCompleted ? "font-medium text-foreground" : "text-muted-foreground/50")}>
                    {step}
                  </span>
                </div>
              </React.Fragment>
            );
          })}
        </div>
      </div>

      <nav className="mb-6">
        <Link
          href="/cart"
          className="inline-flex items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
        >
          <ArrowLeft className="h-4 w-4" />
          {{t.backToCart}}
        </Link>
      </nav>

      <h1 className="text-3xl font-semibold tracking-tight text-foreground sm:text-4xl">{{t.checkout}}</h1>
      <p className="mt-1 text-muted-foreground">
        {{t.completeYourOrder}}
      </p>

      <div className="mt-8">
        <CheckoutForm />
      </div>

      {/* Bottom trust indicators */}
      <div className="mt-8 flex flex-wrap items-center justify-center gap-6 text-xs text-muted-foreground/50">
        <div className="flex items-center gap-1.5">
          <Shield className="h-3.5 w-3.5" />
          <span>{{t.sslEncrypted}}</span>
        </div>
        <div className="flex items-center gap-1.5">
          <Lock className="h-3.5 w-3.5" />
          <span>{{t.securePayment}}</span>
        </div>
      </div>
    </div>
  );
}
