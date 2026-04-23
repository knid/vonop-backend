import React from "react";
import Link from "next/link";
import { cookies } from "next/headers";
import { ShoppingBag } from "lucide-react";
import { getCart, getProducts } from "@/lib/wemob-store-server";
import { ProductCard } from "@/components/ecommerce/product-card";
import { EmptyState } from "@/components/ecommerce/empty-state";
import { CartView } from "@/components/ecommerce/cart-view";

// Cookie set server-side by the checkout / cart-session API route. Until that
// cookie exists we render an empty-cart state — client-side mutations (PR 3)
// will populate it on first write.
const CART_SESSION_COOKIE = "wemob_cart_session";

export default async function CartPage() {
  const cookieStore = await cookies();
  const sessionId = cookieStore.get(CART_SESSION_COOKIE)?.value ?? "";

  const cart = sessionId ? await getCart(sessionId) : null;
  const itemCount = cart ? cart.items.reduce((sum, i) => sum + i.quantity, 0) : 0;

  if (!cart || itemCount === 0) {
    return (
      <div className="mx-auto max-w-7xl px-4 py-12 sm:px-6 sm:py-16 lg:px-8">
        <h1 className="text-3xl font-semibold tracking-tight text-foreground sm:text-4xl">{{t.shoppingCart}}</h1>
        <div className="mt-12">
          <EmptyState
            icon={<ShoppingBag className="h-10 w-10" />}
            title="{{t.emptyCartTitle}}"
            description="{{t.emptyCartPageDesc}}"
            action={<Link href="/products" className="inline-flex h-11 items-center rounded-full bg-foreground px-6 text-sm font-medium text-background transition-opacity hover:opacity-90">{{t.continueShopping}}</Link>}
          />
        </div>
      </div>
    );
  }

  // Suggestions: non-cart, in-stock products. Cap at 4 before passing in.
  const allProducts = await getProducts();
  const cartProductIds = cart.items.map((i) => i.product_id);
  const suggestions = allProducts
    .filter((p) => !cartProductIds.includes(p.id) && p.in_stock !== false)
    .slice(0, 4);

  return (
    <div className="mx-auto max-w-7xl px-4 py-12 sm:px-6 sm:py-16 lg:px-8">
      <CartView cart={cart} itemCount={itemCount} />

      {/* {{t.youMightAlsoLike}} */}
      {suggestions.length > 0 && (
        <div className="mt-16 border-t border-border/40 pt-12">
          <h2 className="text-xl font-semibold tracking-tight text-foreground">{{t.youMightAlsoLike}}</h2>
          <div className="mt-6 grid grid-cols-2 gap-6 sm:gap-8 lg:grid-cols-4">
            {suggestions.map((p) => (
              <ProductCard key={p.id} product={p} />
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
