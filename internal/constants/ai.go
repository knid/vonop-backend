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

Output:
   Return ONLY a JSON array of the selected URLs.
   No explanation. No extra text.
   Always return relative URLs.

### Output format (strict JSON)
{
  "items": [
    { "label": "Home", "url": "/", "icon": "home", "order": 1 },
    { "label": "Account", "url": "/account", "icon": "person", "order": 2 }
    { "label": "Settings", "url": "/settings", "icon": "settings", "order": 3 }
  ]
}
`

	LINK_SELECTION_FOR_SCREENSHOTS_PROMPT = `
You are an expert in web and mobile UX design.
You will receive a list of URLs extracted from a website’s homepage.
Each URL represents a clickable link found somewhere on the page.

Your task:
Select up to 5 URLs that would best represent the variety of content on the site for screenshot generation.

### Reasoning Guidelines
1. Choose URLs that lead to distinct sections or features of the site.
2. Prefer URLs that are likely to have visually interesting content.
3. Prefer: home/landing pages, product lists, product details, features/services pages, galleries, portfolios, blog pages, visually strong sections.
4. Avoid selecting URLs that lead to similar content or layouts.
5. Avoid: login/register/auth pages, account pages, privacy/terms, checkout/cart, admin/wp-admin, error/404 pages, feeds, sitemaps, asset files (.css/.js/.png/.jpg/.svg/.pdf), and URLs with tracking parameters (utm_*, _ga, etc.).
6. Do not select multiple similar pages (ensure diversity).
7. Ensure a mix of content types (e.g., product pages, category pages, informational pages).
8. Exclude URLs that are likely to be login, signup, or legal pages.
9. Choose ONLY from the URLs given.
10. Return between 2 and 5 URLs.


Output:
   Return ONLY a JSON array of the selected URLs.
   No explanation. No extra text.
   Always return absolute or root-relative URLs.
   First item should be the homepage always if present.

### Output format (strict JSON)

{
"selected_urls": [
   "https://example.com/url1",
   "https://example.com/url2",
   "https://example.com/url3"
]
}
`
)
