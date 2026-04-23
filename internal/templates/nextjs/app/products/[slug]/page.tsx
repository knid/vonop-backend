import React from "react";
import Link from "next/link";
import { notFound } from "next/navigation";
import { Truck, RotateCcw, Shield } from "lucide-react";
import { getProduct, getProducts } from "@/lib/wemob-store-server";
import { ProductGallery } from "@/components/ecommerce/product-gallery";
import { PriceDisplay } from "@/components/ecommerce/price-display";
import { ReviewList } from "@/components/ecommerce/review-list";
import { WishlistButton } from "@/components/ecommerce/wishlist-button";
import { ProductCard } from "@/components/ecommerce/product-card";
import { ProductDetailActions } from "@/components/ecommerce/product-detail-actions";

export default async function ProductDetailPage({
  params,
}: {
  params: Promise<{ slug: string }>;
}) {
  const { slug } = await params;
  const product = await getProduct(slug);
  if (!product) {
    notFound();
  }

  // Pull related products from the same category. When the product has no
  // category we skip the extra DB round-trip.
  const related = product.category_slug
    ? (await getProducts({ category: product.category_slug }))
        .filter((p) => p.id !== product.id)
        .slice(0, 4)
    : [];

  return (
    <div className="mx-auto max-w-7xl px-4 py-8 sm:px-6 sm:py-12 lg:px-8">
      {/* Breadcrumbs */}
      <nav className="mb-6 flex items-center gap-2 text-sm text-muted-foreground/50">
        <Link href="/" className="transition-colors hover:text-foreground">Home</Link>
        <span>/</span>
        <Link href="/products" className="transition-colors hover:text-foreground">Products</Link>
        {product.category && (
          <>
            <span>/</span>
            <Link href={"/categories/" + (product.category_slug || product.category.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/-+$/, ""))} className="transition-colors hover:text-foreground">
              {product.category}
            </Link>
          </>
        )}
        <span>/</span>
        <span className="truncate font-medium text-foreground">{product.name}</span>
      </nav>

      <div className="grid gap-8 lg:grid-cols-2 lg:gap-12">
        <ProductGallery images={product.images || []} productName={product.name} />

        <div className="lg:sticky lg:top-24 lg:self-start">
          {product.category && (
            <p className="mb-2 text-[11px] font-medium uppercase tracking-[0.15em] text-muted-foreground/60">
              {product.category}
            </p>
          )}
          <h1 className="text-3xl font-semibold tracking-tight text-foreground sm:text-4xl">
            {product.name}
          </h1>

          <div className="mt-4 flex items-center gap-3">
            <PriceDisplay product={product} size="lg" />
            <WishlistButton productId={product.id} />
          </div>

          {product.description && (
            <div className="mt-6 text-base font-light leading-relaxed text-muted-foreground">
              <p>{product.description}</p>
            </div>
          )}

          {/* Interactive: quantity + add-to-cart + tabs + share (client island) */}
          <ProductDetailActions product={product} />

          {/* Trust Badges */}
          <div className="mt-8 space-y-4">
            <div className="grid grid-cols-3 gap-3">
              <div className="flex flex-col items-center gap-2 rounded-xl border border-border/40 p-3 text-center">
                <Truck className="h-5 w-5 text-muted-foreground" />
                <div>
                  <p className="text-xs font-medium text-foreground">{{t.freeShipping}}</p>
                  <p className="text-[10px] text-muted-foreground">{{t.ordersOver50}}</p>
                </div>
              </div>
              <div className="flex flex-col items-center gap-2 rounded-xl border border-border/40 p-3 text-center">
                <RotateCcw className="h-5 w-5 text-muted-foreground" />
                <div>
                  <p className="text-xs font-medium text-foreground">{{t.easyReturns}}</p>
                  <p className="text-[10px] text-muted-foreground">30-day policy</p>
                </div>
              </div>
              <div className="flex flex-col items-center gap-2 rounded-xl border border-border/40 p-3 text-center">
                <Shield className="h-5 w-5 text-muted-foreground" />
                <div>
                  <p className="text-xs font-medium text-foreground">Secure</p>
                  <p className="text-[10px] text-muted-foreground">SSL encrypted</p>
                </div>
              </div>
            </div>

            {/* {{t.guaranteedSafeCheckout}} */}
            <div className="rounded-xl border border-border/40 p-4">
              <p className="mb-2.5 text-center text-[11px] font-medium uppercase tracking-[0.15em] text-muted-foreground/60">{{t.guaranteedSafeCheckout}}</p>
              <div className="flex items-center justify-center gap-2.5">
                <svg className="h-7 w-auto text-muted-foreground/40" viewBox="0 0 48 32" fill="none"><rect width="48" height="32" rx="4" fill="currentColor" opacity="0.15"/><path d="M19.5 20.5l2-9h3l-2 9h-3zm13-9l-3 6.2-.4-2-1-5.4s-.1-.8-1.2-.8h-4.7l-.1.3s1.4.3 3 1.3l2.3 8.4h3.1l4.7-8h-2.7zm-19 0l-3 9h-3.2l-1.8-7.2c-.1-.4-.2-.6-.5-.7-.6-.3-1.5-.6-2.3-.8l.1-.3h4.8c.6 0 1.2.4 1.3 1.2l1.2 6.2 2.9-7.4h3.5zm26.2 6.2c0-2.5-3.4-2.6-3.4-3.7 0-.3.3-.7 1.1-.8.4 0 1.5-.1 2.7.5l.5-2.2c-.7-.2-1.5-.5-2.6-.5-2.7 0-4.7 1.5-4.7 3.6 0 1.6 1.4 2.4 2.4 3 1.1.5 1.4.8 1.4 1.3 0 .7-.8 1-1.6 1-.7 0-1.9-.3-2.5-.5l-.5 2.2c.6.3 1.6.5 2.7.5 2.9 0 4.8-1.4 4.8-3.7l-.3.3z" fill="currentColor" opacity="0.5"/></svg>
                <svg className="h-7 w-auto text-muted-foreground/40" viewBox="0 0 48 32" fill="none"><rect width="48" height="32" rx="4" fill="currentColor" opacity="0.15"/><circle cx="19" cy="16" r="8" fill="currentColor" opacity="0.2"/><circle cx="29" cy="16" r="8" fill="currentColor" opacity="0.3"/></svg>
                <svg className="h-7 w-auto text-muted-foreground/40" viewBox="0 0 48 32" fill="none"><rect width="48" height="32" rx="4" fill="currentColor" opacity="0.15"/><text x="24" y="20" textAnchor="middle" fontSize="10" fontWeight="bold" fill="currentColor" opacity="0.4">AMEX</text></svg>
                <svg className="h-7 w-auto text-muted-foreground/40" viewBox="0 0 48 32" fill="none"><rect width="48" height="32" rx="4" fill="currentColor" opacity="0.15"/><text x="24" y="19" textAnchor="middle" fontSize="8" fontWeight="600" fontStyle="italic" fill="currentColor" opacity="0.4">PayPal</text></svg>
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* Reviews Section */}
      <div className="mt-20">
        <ReviewList productSlug={product.slug} />
      </div>

      {/* Related Products */}
      {related.length > 0 && (
        <div className="mt-20">
          <h2 className="text-2xl font-semibold tracking-tight text-foreground">{{t.youMayAlsoLike}}</h2>
          <div className="mt-8 grid grid-cols-2 gap-6 sm:gap-8 lg:grid-cols-4">
            {related.map((p) => (
              <ProductCard key={p.id} product={p} />
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
