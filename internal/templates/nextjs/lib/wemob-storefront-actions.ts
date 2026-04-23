"use server";
// lib/wemob-storefront-actions.ts — Read helpers exposed as React Server
// Actions so client components can hit the tenant DB without a bespoke
// /api/* route per resource. Each export becomes an opaque POST endpoint
// Next.js wires up at build time; the client SDK (lib/wemob-store.ts)
// imports these directly and calls them like regular async functions.
// This file is template-managed. Do not modify.

import * as store from "./wemob-store-server";
import type {
  StoreConfig,
  StorefrontCategory,
  StorefrontProduct,
} from "./wemob-store-types";

export async function getStoreConfigAction(): Promise<StoreConfig> {
  return store.getStoreConfig();
}

export async function getProductsAction(params?: {
  category?: string;
  q?: string;
  limit?: number;
  offset?: number;
}): Promise<StorefrontProduct[]> {
  return store.getProducts(params);
}

export async function getProductAction(
  slug: string,
): Promise<StorefrontProduct | null> {
  return store.getProduct(slug);
}

export async function getCategoriesAction(): Promise<StorefrontCategory[]> {
  return store.getCategories();
}
