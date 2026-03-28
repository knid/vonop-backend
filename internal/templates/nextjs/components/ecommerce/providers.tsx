"use client";

import React from "react";
import { CartProvider } from "@/lib/wemob-store";

export function Providers({ children }: { children: React.ReactNode }) {
  return <CartProvider>{children}</CartProvider>;
}
