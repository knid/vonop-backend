"use client";

import React, { useState, useEffect, useRef } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { ShoppingBag, Search, Menu, X, ChevronDown, User } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { useCart, useCategories, useStoreConfig } from "@/lib/wemob-store";
import { CartDrawer } from "@/components/ecommerce/cart-drawer";

export function SiteHeader() {
  const pathname = usePathname();
  const { itemCount } = useCart();
  const { categories } = useCategories();
  const { config: storeConfig } = useStoreConfig();

  const [cartOpen, setCartOpen] = useState(false);
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);
  const [scrolled, setScrolled] = useState(false);
  const [shopOpen, setShopOpen] = useState(false);
  const [mobileShopOpen, setMobileShopOpen] = useState(false);
  const [searchOpen, setSearchOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [announcementVisible, setAnnouncementVisible] = useState(true);
  const shopTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const searchInputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const handleScroll = () => setScrolled(window.scrollY > 10);
    window.addEventListener("scroll", handleScroll, { passive: true });
    return () => window.removeEventListener("scroll", handleScroll);
  }, []);

  useEffect(() => {
    setMobileMenuOpen(false);
    setMobileShopOpen(false);
    setSearchOpen(false);
  }, [pathname]);

  useEffect(() => {
    if (searchOpen && searchInputRef.current) searchInputRef.current.focus();
  }, [searchOpen]);

  const navLinks = [
    { label: "{{t.home}}", href: "/" },
    { label: "{{t.about}}", href: "/about" },
    { label: "{{t.contact}}", href: "/contact" },
  ];

  const shopLinks = [
    { label: "{{t.allProducts}}", href: "/products" },
    ...(categories ?? []).map((cat) => ({
      label: cat.name,
      href: "/categories/" + cat.slug,
    })),
  ];

  const isActive = (href: string) =>
    href === "/" ? pathname === "/" : pathname.startsWith(href);

  const isShopActive = pathname.startsWith("/products") || pathname.startsWith("/categories");

  const handleShopEnter = () => {
    if (shopTimeoutRef.current) clearTimeout(shopTimeoutRef.current);
    setShopOpen(true);
  };

  const handleShopLeave = () => {
    shopTimeoutRef.current = setTimeout(() => setShopOpen(false), 150);
  };

  return (
    <>
      {/* Announcement Bar */}
      <AnimatePresence>
        {announcementVisible && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: "auto", opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.2 }}
            className="relative z-50 overflow-hidden bg-foreground text-background"
          >
            <div className="flex items-center justify-center px-4 py-2 text-center text-xs font-medium tracking-wide sm:text-sm">
              <span>{{t.freeShippingAnnouncement}}</span>
              <span className="mx-2 hidden text-background/40 sm:inline">|</span>
              <Link href="/products" className="hidden underline underline-offset-2 transition-opacity hover:opacity-80 sm:inline">
                {{t.shopNow}}
              </Link>
              <button
                onClick={() => setAnnouncementVisible(false)}
                className="absolute right-3 top-1/2 -translate-y-1/2 rounded-full p-0.5 transition-opacity hover:opacity-70"
                aria-label="{{t.dismissAnnouncement}}"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            </div>
          </motion.div>
        )}
      </AnimatePresence>

      <header
        className={
          "sticky top-0 z-50 w-full transition-all duration-300 " +
          (scrolled
            ? "bg-background/80 backdrop-blur-xl border-b border-border/30 shadow-sm"
            : "bg-background border-b border-transparent")
        }
      >
        <div className={"mx-auto flex max-w-7xl items-center justify-between px-4 transition-all duration-300 sm:px-6 lg:px-8 " + (scrolled ? "h-14" : "h-16")}>
          <Link href="/" className="text-xl font-bold tracking-tight text-foreground">
            {storeConfig?.name}
          </Link>

          <nav className="hidden items-center gap-1 md:flex">
            <Link
              href="/"
              className={"relative px-3 py-2 text-sm font-medium transition-colors " +
                (pathname === "/" ? "text-foreground" : "text-muted-foreground hover:text-foreground")}
            >
              Home
              {pathname === "/" && (
                <motion.div
                  layoutId="nav-underline"
                  className="absolute -bottom-px left-0 right-0 h-0.5 bg-foreground"
                  transition={{ type: "spring", bounce: 0.2, duration: 0.5 }}
                />
              )}
            </Link>

            {/* Shop dropdown */}
            <div
              className="relative"
              onMouseEnter={handleShopEnter}
              onMouseLeave={handleShopLeave}
            >
              <Link
                href="/products"
                className={"relative inline-flex items-center gap-1 px-3 py-2 text-sm font-medium transition-colors " +
                  (isShopActive ? "text-foreground" : "text-muted-foreground hover:text-foreground")}
              >
                Shop
                <ChevronDown className={"h-3.5 w-3.5 transition-transform " + (shopOpen ? "rotate-180" : "")} />
                {isShopActive && (
                  <motion.div
                    layoutId="nav-underline"
                    className="absolute -bottom-px left-0 right-0 h-0.5 bg-foreground"
                    transition={{ type: "spring", bounce: 0.2, duration: 0.5 }}
                  />
                )}
              </Link>

              <AnimatePresence>
                {shopOpen && (
                  <motion.div
                    initial={{ opacity: 0, y: 4 }}
                    animate={{ opacity: 1, y: 0 }}
                    exit={{ opacity: 0, y: 4 }}
                    transition={{ duration: 0.15 }}
                    className="absolute left-0 top-full pt-2"
                  >
                    <div className="w-52 overflow-hidden rounded-xl border border-border/40 bg-background shadow-lg">
                      <div className="py-1.5">
                        {shopLinks.map((link, i) => (
                          <Link
                            key={link.href + "-" + i}
                            href={link.href}
                            className={"block px-4 py-2 text-sm transition-colors " +
                              (isActive(link.href)
                                ? "bg-muted text-foreground font-medium"
                                : "text-muted-foreground hover:bg-muted/50 hover:text-foreground")}
                          >
                            {link.label}
                          </Link>
                        ))}
                      </div>
                    </div>
                  </motion.div>
                )}
              </AnimatePresence>
            </div>

            {navLinks.slice(1).map((link) => (
              <Link
                key={link.href}
                href={link.href}
                className={"relative px-3 py-2 text-sm font-medium transition-colors " +
                  (isActive(link.href) ? "text-foreground" : "text-muted-foreground hover:text-foreground")}
              >
                {link.label}
                {isActive(link.href) && (
                  <motion.div
                    layoutId="nav-underline"
                    className="absolute -bottom-px left-0 right-0 h-0.5 bg-foreground"
                    transition={{ type: "spring", bounce: 0.2, duration: 0.5 }}
                  />
                )}
              </Link>
            ))}
          </nav>

          <div className="flex items-center gap-1">
            {/* Inline Search */}
            <div className="relative hidden md:block">
              <AnimatePresence>
                {searchOpen && (
                  <motion.div
                    initial={{ width: 0, opacity: 0 }}
                    animate={{ width: 220, opacity: 1 }}
                    exit={{ width: 0, opacity: 0 }}
                    transition={{ duration: 0.2 }}
                    className="absolute right-0 top-1/2 -translate-y-1/2 overflow-hidden"
                  >
                    <form action="/products" method="get" className="flex items-center">
                      <input
                        ref={searchInputRef}
                        name="q"
                        type="text"
                        value={searchQuery}
                        onChange={(e) => setSearchQuery(e.target.value)}
                        placeholder="{{t.searchPlaceholder}}"
                        className="h-9 w-full rounded-full border border-border/60 bg-muted/30 pl-4 pr-9 text-sm text-foreground placeholder:text-muted-foreground focus:border-foreground focus:outline-none"
                      />
                      <button type="button" onClick={() => { setSearchOpen(false); setSearchQuery(""); }} className="absolute right-2 top-1/2 -translate-y-1/2 p-0.5 text-muted-foreground hover:text-foreground">
                        <X className="h-3.5 w-3.5" />
                      </button>
                    </form>
                  </motion.div>
                )}
              </AnimatePresence>
              {!searchOpen && (
                <button
                  onClick={() => setSearchOpen(true)}
                  className="flex h-10 w-10 items-center justify-center rounded-full text-muted-foreground transition-colors hover:text-foreground"
                  aria-label="{{t.searchProducts}}"
                >
                  <Search className="h-5 w-5" />
                </button>
              )}
            </div>
            <Link
              href="/products"
              className="flex h-10 w-10 items-center justify-center rounded-full text-muted-foreground transition-colors hover:text-foreground md:hidden"
              aria-label="{{t.searchProducts}}"
            >
              <Search className="h-5 w-5" />
            </Link>

            <Link
              href="/account"
              className="hidden h-10 w-10 items-center justify-center rounded-full text-muted-foreground transition-colors hover:text-foreground sm:flex"
              aria-label="{{t.account}}"
            >
              <User className="h-5 w-5" />
            </Link>

            <button
              onClick={() => setCartOpen(true)}
              className="relative flex h-10 w-10 items-center justify-center rounded-full text-muted-foreground transition-colors hover:text-foreground"
              aria-label="{{t.openCart}}"
            >
              <ShoppingBag className="h-5 w-5" />
              <AnimatePresence>
                {itemCount > 0 && (
                  <motion.span
                    key={itemCount}
                    initial={{ scale: 0 }}
                    animate={{ scale: 1 }}
                    exit={{ scale: 0 }}
                    className="absolute -right-0.5 -top-0.5 flex h-[18px] w-[18px] items-center justify-center rounded-full bg-foreground text-[10px] font-semibold text-background"
                  >
                    {itemCount > 99 ? "99+" : itemCount}
                  </motion.span>
                )}
              </AnimatePresence>
            </button>

            <button
              onClick={() => setMobileMenuOpen(!mobileMenuOpen)}
              className="flex h-10 w-10 items-center justify-center rounded-full text-muted-foreground transition-colors hover:text-foreground md:hidden"
              aria-label={mobileMenuOpen ? "{{t.closeMenu}}" : "{{t.openMenu}}"}
            >
              {mobileMenuOpen ? <X className="h-5 w-5" /> : <Menu className="h-5 w-5" />}
            </button>
          </div>
        </div>
      </header>

      {/* Full-screen mobile menu */}
      <AnimatePresence>
        {mobileMenuOpen && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.2 }}
            className="fixed inset-0 z-40 bg-background md:hidden"
          >
            <div className="flex h-full flex-col items-center justify-center gap-2 px-4">
              <motion.div initial={{ opacity: 0, y: 20 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0 }}>
                <Link
                  href="/"
                  className={"block px-4 py-3 text-center text-2xl font-semibold tracking-tight transition-colors " +
                    (pathname === "/" ? "text-foreground" : "text-muted-foreground hover:text-foreground")}
                >
                  Home
                </Link>
              </motion.div>

              <motion.div initial={{ opacity: 0, y: 20 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0.05 }}>
                <button
                  onClick={() => setMobileShopOpen(!mobileShopOpen)}
                  className={"flex items-center gap-2 px-4 py-3 text-2xl font-semibold tracking-tight transition-colors " +
                    (isShopActive ? "text-foreground" : "text-muted-foreground hover:text-foreground")}
                >
                  Shop
                  <ChevronDown className={"h-5 w-5 transition-transform " + (mobileShopOpen ? "rotate-180" : "")} />
                </button>
                <AnimatePresence>
                  {mobileShopOpen && (
                    <motion.div
                      initial={{ height: 0, opacity: 0 }}
                      animate={{ height: "auto", opacity: 1 }}
                      exit={{ height: 0, opacity: 0 }}
                      transition={{ duration: 0.2 }}
                      className="overflow-hidden"
                    >
                      <div className="flex flex-col items-center gap-1 pb-2">
                        {shopLinks.map((link, i) => (
                          <Link
                            key={link.href + "-" + i}
                            href={link.href}
                            className={"block px-4 py-1.5 text-center text-lg transition-colors " +
                              (isActive(link.href) ? "text-foreground font-medium" : "text-muted-foreground hover:text-foreground")}
                          >
                            {link.label}
                          </Link>
                        ))}
                      </div>
                    </motion.div>
                  )}
                </AnimatePresence>
              </motion.div>

              {navLinks.slice(1).map((link, i) => (
                <motion.div
                  key={link.href}
                  initial={{ opacity: 0, y: 20 }}
                  animate={{ opacity: 1, y: 0 }}
                  transition={{ delay: (i + 2) * 0.05 }}
                >
                  <Link
                    href={link.href}
                    className={"block px-4 py-3 text-center text-2xl font-semibold tracking-tight transition-colors " +
                      (isActive(link.href) ? "text-foreground" : "text-muted-foreground hover:text-foreground")}
                  >
                    {link.label}
                  </Link>
                </motion.div>
              ))}
            </div>
          </motion.div>
        )}
      </AnimatePresence>

      <CartDrawer isOpen={cartOpen} onClose={() => setCartOpen(false)} />
    </>
  );
}
