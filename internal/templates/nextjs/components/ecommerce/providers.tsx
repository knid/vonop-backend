"use client";

import React from "react";
import {
  CartProvider,
  StoreProvider,
  type StoreConfig,
  type StorefrontCategory,
} from "@/lib/wemob-store";

// Seeded from app/layout.tsx on every request. config + categories arrive
// fully materialized so descendants never observe a loading state and
// formatPrice() has a valid currency/locale from the first render.
export function Providers({
  config,
  categories,
  children,
}: {
  config: StoreConfig;
  categories: StorefrontCategory[];
  children: React.ReactNode;
}) {
  return (
    <StoreProvider config={config} categories={categories}>
      <CartProvider>{children}</CartProvider>
    </StoreProvider>
  );
}
