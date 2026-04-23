import React from "react";
import { getCategories, getProducts } from "@/lib/wemob-store-server";
import { ProductsBrowser } from "@/components/ecommerce/products-browser";

export default async function ProductsPage({
  searchParams,
}: {
  searchParams: Promise<{ q?: string; category?: string }>;
}) {
  const sp = await searchParams;
  const q = typeof sp.q === "string" ? sp.q : undefined;
  const category = typeof sp.category === "string" ? sp.category : undefined;

  // Initial server-rendered product set honors ?q and ?category. Further
  // client-side filters (price range, on-sale, in-stock) and sort operate on
  // this seed list without re-querying the DB.
  const [products, categories] = await Promise.all([
    getProducts(q || category ? { q, category } : undefined),
    getCategories(),
  ]);

  return (
    <div className="mx-auto max-w-7xl px-4 py-8 sm:px-6 sm:py-12 lg:px-8">
      <div className="mb-8">
        <h1 className="text-3xl font-bold tracking-tight text-foreground sm:text-4xl">
          All Products
        </h1>
      </div>
      <ProductsBrowser
        initialProducts={products}
        categories={categories}
        initialQuery={q ?? ""}
        initialCategorySlug={category ?? null}
      />
    </div>
  );
}
