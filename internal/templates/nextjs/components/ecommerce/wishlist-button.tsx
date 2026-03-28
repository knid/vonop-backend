"use client";

import React from "react";
import { Heart } from "lucide-react";
import { motion } from "framer-motion";
import { useWishlist } from "@/lib/wemob-store";

interface WishlistButtonProps {
  productId: string;
  size?: "sm" | "md";
  className?: string;
}

export function WishlistButton({ productId, size = "md", className = "" }: WishlistButtonProps) {
  const { isInWishlist, toggleWishlist } = useWishlist();
  const [isToggling, setIsToggling] = React.useState(false);
  const active = isInWishlist(productId);

  const handleClick = async (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (isToggling) return;
    setIsToggling(true);
    try {
      await toggleWishlist(productId);
    } catch {
      // silently handle
    } finally {
      setIsToggling(false);
    }
  };

  const btnSize = size === "sm" ? "h-8 w-8" : "h-10 w-10";
  const iconSize = size === "sm" ? "h-4 w-4" : "h-5 w-5";

  return (
    <motion.button
      onClick={handleClick}
      disabled={isToggling}
      whileTap={{ scale: 0.85 }}
      animate={active ? { scale: [1, 1.15, 1] } : {}}
      transition={{ duration: 0.3 }}
      className={"flex items-center justify-center rounded-full transition-all bg-background/80 backdrop-blur-sm " +
        (active ? "text-red-500 shadow-md shadow-red-500/20" : "shadow-sm text-muted-foreground hover:text-red-500") +
        " " + btnSize + " " + className}
      aria-label={active ? "{{t.removeFromWishlist}}" : "{{t.addToWishlist}}"}
    >
      <Heart className={iconSize + (active ? " fill-current" : "")} />
    </motion.button>
  );
}
