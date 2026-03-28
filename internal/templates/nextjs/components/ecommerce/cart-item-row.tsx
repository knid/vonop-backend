"use client";

import React from "react";
import Image from "next/image";
import { Trash2, ShoppingBag } from "lucide-react";
import { motion } from "framer-motion";
import { useCart, formatPrice, type StorefrontCartItem } from "@/lib/wemob-store";
import { QuantitySelector } from "@/components/ecommerce/quantity-selector";

interface CartItemRowProps {
  item: StorefrontCartItem;
  compact?: boolean;
}

export function CartItemRow({ item, compact = false }: CartItemRowProps) {
  const { updateItem, removeItem } = useCart();
  const [isRemoving, setIsRemoving] = React.useState(false);

  const handleQuantityChange = async (qty: number) => {
    try {
      await updateItem(item.id, qty);
    } catch {
      // silently handle — optimistic update will rollback
    }
  };

  const handleRemove = async () => {
    setIsRemoving(true);
    try {
      await removeItem(item.id);
    } catch {
      setIsRemoving(false);
    }
  };

  return (
    <motion.div
      layout
      exit={{ opacity: 0, x: -20 }}
      transition={{ duration: 0.2 }}
      className={"group flex gap-3 " + (compact ? "rounded-xl p-2" : "rounded-xl border border-border/40 p-4 hover:border-border/70 transition-colors") + (isRemoving ? " opacity-50" : "")}
    >
      <div className={"relative flex-shrink-0 overflow-hidden rounded-xl bg-muted " + (compact ? "h-16 w-16" : "h-20 w-20")}>
        {item.product_image ? (
          <Image src={item.product_image} alt={item.product_name} fill className="object-cover" sizes={compact ? "64px" : "80px"} />
        ) : (
          <div className="flex h-full w-full items-center justify-center">
            <ShoppingBag className="h-5 w-5 text-muted-foreground/40" />
          </div>
        )}
      </div>
      <div className="flex flex-1 flex-col justify-between">
        <div>
          <h3 className={"font-medium " + (compact ? "text-sm" : "text-base")}>{item.product_name}</h3>
        </div>
        <div className="flex items-center justify-between">
          <QuantitySelector value={item.quantity} onChange={handleQuantityChange} size={compact ? "sm" : "md"} />
          <div className="flex items-center gap-3">
            <span className={"font-semibold " + (compact ? "text-sm" : "text-base")}>{formatPrice(item.total_cents)}</span>
            <button onClick={handleRemove} disabled={isRemoving} className="text-muted-foreground opacity-0 transition-all hover:text-destructive group-hover:opacity-100" aria-label={"Remove " + item.product_name}>
              <Trash2 className={compact ? "h-3.5 w-3.5" : "h-4 w-4"} />
            </button>
          </div>
        </div>
      </div>
    </motion.div>
  );
}
