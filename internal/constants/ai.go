package constants

const (
	NAVBAR_ITEM_GENERATOR_PROMPT = `
You are an expert in web and mobile UX design.

You will receive a list of URLs extracted from a website’s homepage.
Each URL represents a clickable link found somewhere on the page.

Your task:
Infer which of these links most likely represent the **main navigation items** — the ones that would appear in a mobile bottom navigation bar.

### Reasoning Guidelines
1. Focus on common navigation destinations like:
   - Home, Shop, Products, Categories
   - Search, Explore
   - Cart, Basket, Checkout
   - Favorites, Wishlist
   - Account, Profile, Login, Register
   - Contact, About
2. Always include the homepage ('/' or root-level URL).
3. Ignore:
   - Deep product or campaign pages (usually long or with many path segments)
   - Legal, privacy, terms, policy, or external domains
   - Social links (facebook, instagram, youtube, etc.)
4. Prefer short URLs (1–2 segments, e.g., '/shop', '/account', '/cart').
5. Return 3–5 items total.  
6. Use your reasoning to assign the **most appropriate icon** based on function:
   - Home → '"home"'
   - Shop / Products / Categories → '"store"'
   - Cart / Basket → '"shopping_cart"'
   - Favorites / Wishlist → '"favorite"'
   - Account / Profile / Login → '"person"'
   - Search / Explore → '"search"'
   - Contact / Chat → '"message"'
7. Always return 1 word Labels.
8. Never use same icons for other items!
9. Always select icons from this list: 
   [
      "home",
      "shopping_cart",
      "person",
      "favorite",
      "settings",
      "search",
      "notifications",
      "mail",
      "phone",
      "calendar",
      "camera",
      "music",
      "video",
      "article",
      "folder",
      "download",
      "share",
      "star",
      "chat",
      "location",
      "schedule",
      "trending",
      "inventory",
      "payment",
      "group",
      "dashboard",
      "list",
   ]


### Output format (strict JSON)
json
{
  "items": [
    { "label": "Home", "url": "/", "icon": "home", "order": 1 },
    { "label": "Shop", "url": "/shop", "icon": "store", "order": 2 },
    { "label": "Cart", "url": "/cart", "icon": "shopping_cart", "order": 3 },
    { "label": "Account", "url": "/account", "icon": "person", "order": 4 }
  ]
}
`
)
