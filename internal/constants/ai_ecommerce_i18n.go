package constants

import "strings"

// ai_ecommerce_i18n.go — Localized string maps for pre-built e-commerce templates.
// Templates use {{t.key}} placeholders which are replaced with the target language
// at Go template-resolution time. The generated user app has zero i18n overhead.

// ecommerceStrings holds all user-facing strings keyed by language.
// For simple text, the value is just the translated string.
// For JS expressions where word order changes between languages, the value is
// the full JS expression to splice into the template.
var ecommerceStrings = map[string]map[string]string{
	"en": {
		// Product card & common
		"outOfStock": "Out of Stock",
		"sale":       "Sale",

		// Cart drawer
		"cart":                    "Cart",
		"closeCart":               "Close cart",
		"emptyCartTitle":          "Your cart is empty",
		"emptyCartDesc":           "Add some items to get started",
		"qualifyFreeShipping":     "You qualify for free shipping!",
		"subtotal":                "Subtotal",
		"discount":                "Discount",
		"total":                   "Total",
		"taxesShippingAtCheckout": "Taxes and shipping calculated at checkout",
		"secureCheckout":          "Secure Checkout",
		"viewCart":                "View Cart",

		// Quantity selector
		"decreaseQuantity": "Decrease quantity",
		"increaseQuantity": "Increase quantity",

		// Product gallery
		"previousImage": "Previous image",
		"nextImage":     "Next image",

		// Checkout form
		"emptyCartCheckout":    "Your cart is empty. Add items before checking out.",
		"expressCheckout":      "Express Checkout",
		"orContinueWith":       "or continue with",
		"stepInformation":      "Information",
		"stepShipping":         "Shipping",
		"stepPayment":          "Payment",
		"contactInformation":   "Contact Information",
		"email":                "Email",
		"fullName":             "Full Name",
		"shippingAddress":      "Shipping Address",
		"addressLine1":         "Address Line 1",
		"addressLine2Optional": "Address Line 2 (Optional)",
		"city":                 "City",
		"stateProvince":        "State / Province",
		"postalCode":           "Postal Code",
		"country":              "Country",
		"paymentMethod":        "Payment Method",
		"cashOnDelivery":       "Cash on Delivery",
		"paymentNotConfigured": "Payment not yet configured \u2014 contact the store owner.",
		"processing":           "Processing...",
		"placeOrder":           "Place Order",
		"infoEncryptedSecure":  "Your information is encrypted and secure",
		"checkoutFailed":       "Checkout failed. Please try again.",

		// Category nav
		"all": "All",

		// Order summary
		"orderSummary":         "Order Summary",
		"shipping":             "Shipping",
		"calculatedAtCheckout": "Calculated at checkout",

		// Review list
		"customerReviews": "Customer Reviews",
		"writeReview":     "Write a Review",
		"reviewThanks":    "Thank you for your review! It has been submitted successfully.",
		"rating":          "Rating",
		"name":            "Name",
		"titleOptional":   "Title (optional)",
		"review":          "Review",
		"submitReview":    "Submit Review",
		"cancel":          "Cancel",
		"noReviewsYet":    "No reviews yet. Be the first to review this product!",
		"verified":        "Verified",

		// Wishlist
		"addToWishlist":     "Add to wishlist",
		"removeFromWishlist": "Remove from wishlist",

		// Site header
		"freeShippingAnnouncement": "Free shipping on orders over $50",
		"shopNow":                  "Shop Now",
		"dismissAnnouncement":      "Dismiss announcement",
		"home":                     "Home",
		"about":                    "About",
		"contact":                  "Contact",
		"allProducts":              "All Products",
		"shop":                     "Shop",
		"searchPlaceholder":        "Search...",
		"searchProducts":           "Search products",
		"account":                  "Account",
		"openCart":                  "Open cart",
		"closeMenu":                "Close menu",
		"openMenu":                 "Open menu",

		// Site footer
		"footerDescription":    "Quality products curated for you. Shop with confidence and enjoy a seamless shopping experience.",
		"newsletter":           "Newsletter",
		"newsletterDesc":       "Get the latest updates, exclusive offers, and new arrivals delivered to your inbox.",
		"thanksSubscribing":    "Thank you for subscribing!",
		"subscribe":            "Subscribe",
		"company":              "Company",
		"support":              "Support",
		"faq":                  "FAQ",
		"shippingPolicy":       "Shipping Policy",
		"returns":              "Returns",
		"legal":                "Legal",
		"privacyPolicy":        "Privacy Policy",
		"termsOfService":       "Terms of Service",
		"allRightsReserved":    "All rights reserved.",
		"weAccept":             "We accept",
		"secureShoppingFooter": "Secure shopping guaranteed. All transactions are encrypted and your data is protected.",
		"backToTop":            "Back to top",
		"viewAll":              "View All \u2192",

		// Newsletter section
		"newsletterSubtitle": "Join 10,000+ happy customers",
		"stayInTheLoop":      "Stay in the Loop",
		"newsletterCTA":      "Subscribe for exclusive deals, new arrivals, and style inspiration.",
		"enterYourEmail":     "Enter your email",
		"subscribing":        "Subscribing...",
		"invalidEmail":       "Please enter a valid email address.",
		"subscribeSuccess":   "Thank you for subscribing! Check your inbox for a confirmation.",
		"noSpam":             "No spam, ever. Unsubscribe at any time.",

		// Cart page
		"shoppingCart":        "Shopping Cart",
		"continueShopping":    "Continue Shopping",
		"emptyCartPageDesc":   "Looks like you haven't added anything to your cart yet.",
		"discountCode":        "Discount Code",
		"enterCode":           "Enter code",
		"invalidDiscountCode": "Invalid or expired discount code.",
		"applied":             "Applied",
		"applying":            "Applying...",
		"apply":               "Apply",
		"remove":              "Remove",
		"freeShippingOver50":  "Free shipping over $50",
		"estimatedDelivery":   "Estimated delivery",
		"youMightAlsoLike":    "You Might Also Like",

		// Checkout page
		"stepCart":              "Cart",
		"secureCheckoutBanner": "Secure Checkout \u2014 Your information is encrypted and protected",
		"backToCart":            "Back to Cart",
		"checkout":             "Checkout",
		"completeYourOrder":    "Complete your order by filling in the details below.",
		"sslEncrypted":         "SSL Encrypted",
		"securePayment":        "Secure Payment",

		// Checkout success
		"loadingOrderDetails":    "Loading order details...",
		"orderNotFound":          "Order not found",
		"orderNotFoundDesc":      "We couldn't find this order. It may have expired or the link is invalid.",
		"noOrderId":              "No order ID provided.",
		"orderConfirmed":         "Order Confirmed!",
		"thankYouPurchase":       "Thank you for your purchase. Your order number is",
		"qty":                    "Qty",
		"whatHappensNext":        "What Happens Next",
		"stepOrderConfirmed":     "Order Confirmed",
		"stepOrderConfirmedDesc": "We have received your order and are preparing it.",
		"stepProcessing":         "Processing",
		"stepProcessingDesc":     "Your items are being packed with care.",
		"stepShipped":            "Shipped",
		"stepShippedDesc":        "You will receive a tracking number via email.",
		"stepDelivered":          "Delivered",
		"stepDeliveredDesc":      "Your order arrives at your doorstep.",
		"needHelpOrder":          "Need help with your order?",

		// Checkout cancel
		"paymentCancelled":       "Your payment was not completed. Your cart is still saved and no charges were made.",
		"paymentCancelledTitle":  "Payment Cancelled",
		"orderNotCompleted":      "was not completed.",
		"contactSupport":         "Contact Support",
		"cartSavedQuestion":      "Is my cart still saved?",
		"cartSavedAnswer":        "Yes. Your cart items are saved and will be available when you return to checkout.",
		"paymentChargedQuestion": "Was I charged?",
		"paymentChargedAnswer":   "No. Your payment was not processed and no charges were made to your account.",
		"tryDifferentQuestion":   "Can I try a different payment method?",
		"tryDifferentAnswer":     "Absolutely. Return to checkout and select a different payment option.",
		"returnToCheckout":       "Return to Checkout",
		"commonQuestions":        "Common Questions",

		// Products page
		"products":         "Products",
		"sortByFeatured":   "Featured",
		"sortByNewest":     "Newest",
		"sortByPriceLow":   "Price: Low to High",
		"sortByPriceHigh":  "Price: High to Low",
		"filters":          "Filters",
		"clearAll":         "Clear all",
		"category":         "Category",
		"allCategories":    "All Categories",
		"priceRange":       "Price Range",
		"minPrice":         "Min",
		"maxPrice":         "Max",
		"onSale":           "On Sale",
		"inStockOnly":      "In Stock Only",
		"noProductsTitle":  "No products found",
		"noProductsDesc":   "Try adjusting your filters or search terms.",
		"clearFilters":     "Clear Filters",

		// Product detail
		"quantity":              "Quantity",
		"addToCart":             "Add to Cart",
		"addedToCart":           "Added to Cart",
		"adding":               "Adding...",
		"description":          "Description",
		"shippingAndReturns":   "Shipping & Returns",
		"returnsPolicy30d":     "Returns accepted within 30 days of delivery. Items must be unused and in original packaging.",
		"freeShipping":         "Free Shipping",
		"ordersOver50":         "Orders over $50",
		"easyReturns":          "Easy Returns",
		"returnsPolicyShort":   "30-day policy",
		"secureLabel":          "Secure",
		"sslEncryptedShort":    "SSL encrypted",
		"freeShippingExpress":  "Free standard shipping. Express shipping available at checkout.",
		"guaranteedSafeCheckout": "Guaranteed Safe Checkout",
		"youMayAlsoLike":       "You May Also Like",

		// Category page
		"noCategoryProducts": "No products in this category yet.",
		"backToProducts":     "Back to Products",
		"sortBy":             "Sort by",

		// Policy pages
		"shippingPolicyTitle":          "Shipping Policy",
		"returnsPolicyTitle":           "Returns & Refund Policy",
		"privacyPolicyTitle":           "Privacy Policy",
		"termsPolicyTitle":             "Terms of Service",
		"processingTime":               "Processing Time",
		"processingTimeContent":        "All orders are processed within 1\u20133 business days. Orders placed on weekends or holidays will be processed on the next business day. You will receive a confirmation email with tracking information once your order has shipped.",
		"internationalShipping":        "International Shipping",
		"internationalShippingContent": "We currently ship to select international destinations. International shipping rates and delivery times vary by location and will be calculated at checkout. Please note that international orders may be subject to customs duties and taxes, which are the responsibility of the recipient.",
		"orderTracking":                "Order Tracking",
		"orderTrackingContent":         "Once your order ships, you will receive an email with a tracking number. You can use this number to track your package on the carrier\u2019s website. If you have any questions about your shipment, please don\u2019t hesitate to contact our support team.",
		"shippingRestrictions":         "Shipping Restrictions",
		"shippingRestrictionsContent":  "Some items may have shipping restrictions due to size, weight, or destination regulations. If an item cannot be shipped to your address, you will be notified during checkout. We are unable to ship to P.O. boxes for express and overnight orders.",
		"shippingRates":                "Shipping Rates",
		"shippingRatesAndDelivery":     "Shipping Rates & Delivery Times",
		"method":                       "Method",
		"deliveryTime":                 "Delivery Time",
		"cost":                         "Cost",
		"standardShipping":             "Standard Shipping",
		"standardShippingTime":         "5\u20137 business days",
		"standardShippingCost":         "Free on orders over $50",
		"expressShipping":              "Express Shipping",
		"expressShippingTime":          "2\u20133 business days",
		"expressShippingCost":          "$9.99",
		"overnightShipping":            "Overnight Shipping",
		"overnightShippingTime":        "1 business day",
		"overnightShippingCost":        "$19.99",
		"returnsEligibility":           "Eligibility",
		"returnsEligibility1":          "Items must be returned within 30 days of delivery.",
		"returnsEligibility2":          "Products must be in their original condition, unworn, unwashed, and with all tags attached.",
		"returnsEligibility3":          "Items must be in their original packaging when possible.",
		"returnsEligibility4":          "Sale items and gift cards are final sale and cannot be returned.",
		"returnsEligibility5":          "Personalized or custom-made items are non-returnable.",
		"returnsRefunds":               "Refunds",
		"returnsRefundsContent":        "Refunds will be issued to the original payment method. Please allow 5\u20137 business days for the refund to appear on your statement after we have received and processed your return. Shipping costs are non-refundable unless the return is due to a defective or incorrect item.",
		"privacyIntro":                 "is committed to protecting your privacy. This Privacy Policy explains how we collect, use, disclose, and safeguard your information when you visit our website and make purchases from our store.",
		"informationWeCollect":         "Information We Collect",
		"howWeUseInfo":                 "How We Use Your Information",
		"privacyContact":               "Contact Us",
		"privacyContactContent":        "If you have questions about this Privacy Policy or our data practices, please contact us through our contact page.",
		"termsAcceptance":              "Acceptance of Terms",
		"termsAcceptanceContent":       "By accessing and using this website, you agree to be bound by these Terms of Service and all applicable laws and regulations. If you do not agree with any of these terms, you are prohibited from using this site.",
		"termsOrdersPayment":           "Orders & Payment",
		"termsOrdersPaymentContent":    "By placing an order, you represent that the information provided is accurate and that you are authorized to use the payment method. We reserve the right to refuse or cancel any order for reasons including product availability, errors in pricing or product information, or suspected fraudulent activity.",
		"termsGoverningLaw":            "Governing Law",
		"termsGoverningLawContent":     "These Terms of Service shall be governed by and construed in accordance with applicable local laws, without regard to conflict of law principles.",
		"termsChanges":                 "Changes to Terms",
		"termsChangesContent":          "We reserve the right to update these Terms of Service at any time. Changes will be effective immediately upon posting. Your continued use of the website after changes constitutes acceptance of the updated terms.",
	},

	"tr": {
		// Product card & common
		"outOfStock": "Stokta Yok",
		"sale":       "\u0130ndirim",

		// Cart drawer
		"cart":                    "Sepet",
		"closeCart":               "Sepeti kapat",
		"emptyCartTitle":          "Sepetiniz bo\u015f",
		"emptyCartDesc":           "Ba\u015flamak i\u00e7in \u00fcr\u00fcn ekleyin",
		"qualifyFreeShipping":     "\u00dccretsiz kargoya hak kazand\u0131n\u0131z!",
		"subtotal":                "Ara Toplam",
		"discount":                "\u0130ndirim",
		"total":                   "Toplam",
		"taxesShippingAtCheckout": "Vergiler ve kargo \u00f6deme s\u0131ras\u0131nda hesaplan\u0131r",
		"secureCheckout":          "G\u00fcvenli \u00d6deme",
		"viewCart":                "Sepeti G\u00f6r\u00fcnt\u00fcle",

		// Quantity selector
		"decreaseQuantity": "Miktar\u0131 azalt",
		"increaseQuantity": "Miktar\u0131 art\u0131r",

		// Product gallery
		"previousImage": "\u00d6nceki g\u00f6rsel",
		"nextImage":     "Sonraki g\u00f6rsel",

		// Checkout form
		"emptyCartCheckout":    "Sepetiniz bo\u015f. \u00d6deme yapmadan \u00f6nce \u00fcr\u00fcn ekleyin.",
		"expressCheckout":      "H\u0131zl\u0131 \u00d6deme",
		"orContinueWith":       "veya devam edin",
		"stepInformation":      "Bilgiler",
		"stepShipping":         "Kargo",
		"stepPayment":          "\u00d6deme",
		"contactInformation":   "\u0130leti\u015fim Bilgileri",
		"email":                "E-posta",
		"fullName":             "Ad Soyad",
		"shippingAddress":      "Teslimat Adresi",
		"addressLine1":         "Adres Sat\u0131r\u0131 1",
		"addressLine2Optional": "Adres Sat\u0131r\u0131 2 (Opsiyonel)",
		"city":                 "\u015eehir",
		"stateProvince":        "\u0130l / B\u00f6lge",
		"postalCode":           "Posta Kodu",
		"country":              "\u00dclke",
		"paymentMethod":        "\u00d6deme Y\u00f6ntemi",
		"cashOnDelivery":       "Kap\u0131da \u00d6deme",
		"paymentNotConfigured": "\u00d6deme hen\u00fcz yap\u0131land\u0131r\u0131lmad\u0131 \u2014 ma\u011faza sahibiyle ileti\u015fime ge\u00e7in.",
		"processing":           "\u0130\u015fleniyor...",
		"placeOrder":           "Sipari\u015fi Onayla",
		"infoEncryptedSecure":  "Bilgileriniz \u015fifrelenmi\u015f ve g\u00fcvendedir",
		"checkoutFailed":       "\u00d6deme ba\u015far\u0131s\u0131z oldu. L\u00fctfen tekrar deneyin.",

		// Category nav
		"all": "T\u00fcm\u00fc",

		// Order summary
		"orderSummary":         "Sipari\u015f \u00d6zeti",
		"shipping":             "Kargo",
		"calculatedAtCheckout": "\u00d6deme s\u0131ras\u0131nda hesaplan\u0131r",

		// Review list
		"customerReviews": "M\u00fc\u015fteri Yorumlar\u0131",
		"writeReview":     "Yorum Yaz",
		"reviewThanks":    "Yorumunuz i\u00e7in te\u015fekk\u00fcr ederiz! Ba\u015far\u0131yla g\u00f6nderildi.",
		"rating":          "Puan",
		"name":            "Ad",
		"titleOptional":   "Ba\u015fl\u0131k (opsiyonel)",
		"review":          "Yorum",
		"submitReview":    "Yorumu G\u00f6nder",
		"cancel":          "\u0130ptal",
		"noReviewsYet":    "Hen\u00fcz yorum yok. Bu \u00fcr\u00fcn\u00fc ilk de\u011ferlendiren siz olun!",
		"verified":        "Do\u011frulanm\u0131\u015f",

		// Wishlist
		"addToWishlist":     "\u0130stek listesine ekle",
		"removeFromWishlist": "\u0130stek listesinden \u00e7\u0131kar",

		// Site header
		"freeShippingAnnouncement": "50\u20ba \u00fczeri sipari\u015flerde \u00fccretsiz kargo",
		"shopNow":                  "Al\u0131\u015fveri\u015fe Ba\u015fla",
		"dismissAnnouncement":      "Duyuruyu kapat",
		"home":                     "Ana Sayfa",
		"about":                    "Hakk\u0131m\u0131zda",
		"contact":                  "\u0130leti\u015fim",
		"allProducts":              "T\u00fcm \u00dcr\u00fcnler",
		"shop":                     "Ma\u011faza",
		"searchPlaceholder":        "Ara...",
		"searchProducts":           "\u00dcr\u00fcn ara",
		"account":                  "Hesap",
		"openCart":                  "Sepeti a\u00e7",
		"closeMenu":                "Men\u00fcy\u00fc kapat",
		"openMenu":                 "Men\u00fcy\u00fc a\u00e7",

		// Site footer
		"footerDescription":    "Sizin i\u00e7in se\u00e7ilmi\u015f kaliteli \u00fcr\u00fcnler. G\u00fcvenle al\u0131\u015fveri\u015f yap\u0131n ve kusursuz bir deneyimin keyfini \u00e7\u0131kar\u0131n.",
		"newsletter":           "B\u00fclten",
		"newsletterDesc":       "En son g\u00fcncellemeleri, \u00f6zel f\u0131rsatlar\u0131 ve yeni \u00fcr\u00fcnleri e-postan\u0131za al\u0131n.",
		"thanksSubscribing":    "Abone oldu\u011funuz i\u00e7in te\u015fekk\u00fcrler!",
		"subscribe":            "Abone Ol",
		"company":              "\u015eirket",
		"support":              "Destek",
		"faq":                  "SSS",
		"shippingPolicy":       "Kargo Politikas\u0131",
		"returns":              "\u0130ade",
		"legal":                "Yasal",
		"privacyPolicy":        "Gizlilik Politikas\u0131",
		"termsOfService":       "Kullan\u0131m Ko\u015fullar\u0131",
		"allRightsReserved":    "T\u00fcm haklar\u0131 sakl\u0131d\u0131r.",
		"weAccept":             "Kabul ediyoruz",
		"secureShoppingFooter": "G\u00fcvenli al\u0131\u015fveri\u015f garantisi. T\u00fcm i\u015flemler \u015fifrelenir ve verileriniz korunur.",
		"backToTop":            "Yukar\u0131 d\u00f6n",
		"viewAll":              "T\u00fcm\u00fcn\u00fc G\u00f6r \u2192",

		// Newsletter section
		"newsletterSubtitle": "10.000+ mutlu m\u00fc\u015fteriye kat\u0131l\u0131n",
		"stayInTheLoop":      "Geli\u015fmelerden Haberdar Olun",
		"newsletterCTA":      "\u00d6zel f\u0131rsatlar, yeni \u00fcr\u00fcnler ve ilham verici i\u00e7erikler i\u00e7in abone olun.",
		"enterYourEmail":     "E-posta adresiniz",
		"subscribing":        "Abone olunuyor...",
		"invalidEmail":       "L\u00fctfen ge\u00e7erli bir e-posta adresi girin.",
		"subscribeSuccess":   "Abone oldu\u011funuz i\u00e7in te\u015fekk\u00fcrler! Onay i\u00e7in gelen kutunuzu kontrol edin.",
		"noSpam":             "Spam yok. \u0130stedi\u011finiz zaman abonelikten \u00e7\u0131kabilirsiniz.",

		// Cart page
		"shoppingCart":        "Al\u0131\u015fveri\u015f Sepeti",
		"continueShopping":    "Al\u0131\u015fveri\u015fe Devam Et",
		"emptyCartPageDesc":   "Hen\u00fcz sepetinize bir \u015fey eklemediniz.",
		"discountCode":        "\u0130ndirim Kodu",
		"enterCode":           "Kod girin",
		"invalidDiscountCode": "Ge\u00e7ersiz veya s\u00fcresi dolmu\u015f indirim kodu.",
		"applied":             "Uyguland\u0131",
		"applying":            "Uygulan\u0131yor...",
		"apply":               "Uygula",
		"remove":              "Kald\u0131r",
		"freeShippingOver50":  "50\u20ba \u00fczeri \u00fccretsiz kargo",
		"estimatedDelivery":   "Tahmini teslimat",
		"youMightAlsoLike":    "Bunlar\u0131 da Be\u011fenebilirsiniz",

		// Checkout page
		"stepCart":              "Sepet",
		"secureCheckoutBanner": "G\u00fcvenli \u00d6deme \u2014 Bilgileriniz \u015fifrelenmektedir",
		"backToCart":            "Sepete D\u00f6n",
		"checkout":             "\u00d6deme",
		"completeYourOrder":    "A\u015fa\u011f\u0131daki bilgileri doldurarak sipari\u015finizi tamamlay\u0131n.",
		"sslEncrypted":         "SSL \u015eifreleme",
		"securePayment":        "G\u00fcvenli \u00d6deme",

		// Checkout success
		"loadingOrderDetails":    "Sipari\u015f bilgileri y\u00fckleniyor...",
		"orderNotFound":          "Sipari\u015f bulunamad\u0131",
		"orderNotFoundDesc":      "Bu sipari\u015fi bulamad\u0131k. S\u00fcresi dolmu\u015f veya ba\u011flant\u0131 ge\u00e7ersiz olabilir.",
		"noOrderId":              "Sipari\u015f kimli\u011fi belirtilmedi.",
		"orderConfirmed":         "Sipari\u015f Onayland\u0131!",
		"thankYouPurchase":       "Al\u0131\u015fveri\u015finiz i\u00e7in te\u015fekk\u00fcrler. Sipari\u015f numaran\u0131z",
		"qty":                    "Adet",
		"whatHappensNext":        "Sonraki Ad\u0131mlar",
		"stepOrderConfirmed":     "Sipari\u015f Onayland\u0131",
		"stepOrderConfirmedDesc": "Sipari\u015finizi ald\u0131k ve haz\u0131rl\u0131yoruz.",
		"stepProcessing":         "Haz\u0131rlan\u0131yor",
		"stepProcessingDesc":     "\u00dcr\u00fcnleriniz \u00f6zenle paketleniyor.",
		"stepShipped":            "Kargoya Verildi",
		"stepShippedDesc":        "Takip numaran\u0131z e-posta ile g\u00f6nderilecektir.",
		"stepDelivered":          "Teslim Edildi",
		"stepDeliveredDesc":      "Sipari\u015finiz kap\u0131n\u0131za ula\u015f\u0131r.",
		"needHelpOrder":          "Sipari\u015finizle ilgili yard\u0131ma m\u0131 ihtiyac\u0131n\u0131z var?",

		// Checkout cancel
		"paymentCancelled":       "\u00d6demeniz tamamlanmad\u0131. Sepetiniz kaydedildi ve herhangi bir \u00fccret kesilmedi.",
		"paymentCancelledTitle":  "\u00d6deme \u0130ptal Edildi",
		"orderNotCompleted":      "tamamlanmad\u0131.",
		"contactSupport":         "Deste\u011fe Ula\u015f",
		"cartSavedQuestion":      "Sepetim kaydedildi mi?",
		"cartSavedAnswer":        "Evet. Sepetinizdeki \u00fcr\u00fcnler kaydedilmi\u015ftir ve \u00f6demeye d\u00f6nd\u00fc\u011f\u00fcn\u00fczde haz\u0131r olacakt\u0131r.",
		"paymentChargedQuestion": "\u00dccret kesildi mi?",
		"paymentChargedAnswer":   "Hay\u0131r. \u00d6demeniz i\u015flenmedi ve hesab\u0131n\u0131zdan herhangi bir \u00fccret kesilmedi.",
		"tryDifferentQuestion":   "Farkl\u0131 bir \u00f6deme y\u00f6ntemi deneyebilir miyim?",
		"tryDifferentAnswer":     "Elbette. \u00d6demeye d\u00f6n\u00fcn ve farkl\u0131 bir \u00f6deme se\u00e7ene\u011fi belirleyin.",
		"returnToCheckout":       "\u00d6demeye D\u00f6n",
		"commonQuestions":        "S\u0131k Sorulan Sorular",

		// Products page
		"products":         "\u00dcr\u00fcnler",
		"sortByFeatured":   "\u00d6ne \u00c7\u0131kanlar",
		"sortByNewest":     "En Yeniler",
		"sortByPriceLow":   "Fiyat: D\u00fc\u015f\u00fckten Y\u00fckseke",
		"sortByPriceHigh":  "Fiyat: Y\u00fcsekten D\u00fc\u015f\u00fc\u011fe",
		"filters":          "Filtreler",
		"clearAll":         "T\u00fcm\u00fcn\u00fc Temizle",
		"category":         "Kategori",
		"allCategories":    "T\u00fcm Kategoriler",
		"priceRange":       "Fiyat Aral\u0131\u011f\u0131",
		"minPrice":         "Min",
		"maxPrice":         "Maks",
		"onSale":           "\u0130ndirimli",
		"inStockOnly":      "Sadece Stokta Olanlar",
		"noProductsTitle":  "\u00dcr\u00fcn bulunamad\u0131",
		"noProductsDesc":   "Filtrelerinizi veya arama terimlerinizi de\u011fi\u015ftirmeyi deneyin.",
		"clearFilters":     "Filtreleri Temizle",

		// Product detail
		"quantity":              "Miktar",
		"addToCart":             "Sepete Ekle",
		"addedToCart":           "Sepete Eklendi",
		"adding":               "Ekleniyor...",
		"description":          "A\u00e7\u0131klama",
		"shippingAndReturns":   "Kargo & \u0130ade",
		"returnsPolicy30d":     "\u0130adeler teslimat tarihinden itibaren 30 g\u00fcn i\u00e7inde kabul edilir. \u00dcr\u00fcnler kullan\u0131lmam\u0131\u015f ve orijinal ambalaj\u0131nda olmal\u0131d\u0131r.",
		"freeShipping":         "\u00dccretsiz Kargo",
		"ordersOver50":         "50\u20ba \u00fczeri sipari\u015flerde",
		"easyReturns":          "Kolay \u0130ade",
		"returnsPolicyShort":   "30 g\u00fcn i\u00e7inde",
		"secureLabel":          "G\u00fcvenli",
		"sslEncryptedShort":    "SSL \u015fifreleme",
		"freeShippingExpress":  "\u00dccretsiz standart kargo. \u00d6demede h\u0131zl\u0131 kargo se\u00e7ene\u011fi mevcut.",
		"guaranteedSafeCheckout": "G\u00fcvenli \u00d6deme Garantisi",
		"youMayAlsoLike":       "Bunlar\u0131 da Be\u011fenebilirsiniz",

		// Category page
		"noCategoryProducts": "Bu kategoride hen\u00fcz \u00fcr\u00fcn yok.",
		"backToProducts":     "\u00dcr\u00fcnlere D\u00f6n",
		"sortBy":             "S\u0131rala",

		// Policy pages
		"shippingPolicyTitle":          "Kargo Politikas\u0131",
		"returnsPolicyTitle":           "\u0130ade ve Geri \u00d6deme Politikas\u0131",
		"privacyPolicyTitle":           "Gizlilik Politikas\u0131",
		"termsPolicyTitle":             "Kullan\u0131m Ko\u015fullar\u0131",
		"processingTime":               "\u0130\u015fleme S\u00fcresi",
		"processingTimeContent":        "T\u00fcm sipari\u015fler 1\u20133 i\u015f g\u00fcn\u00fc i\u00e7inde i\u015flenir. Hafta sonlar\u0131 ve tatillerde verilen sipari\u015fler bir sonraki i\u015f g\u00fcn\u00fc i\u015flenir. Sipari\u015finiz kargoya verildikten sonra takip bilgilerini i\u00e7eren bir onay e-postas\u0131 alacaks\u0131n\u0131z.",
		"internationalShipping":        "Uluslararas\u0131 Kargo",
		"internationalShippingContent": "\u015eu anda se\u00e7ili uluslararas\u0131 destinasyonlara g\u00f6nderi yapabiliyoruz. Uluslararas\u0131 kargo \u00fccretleri ve teslimat s\u00fcreleri konuma g\u00f6re de\u011fi\u015fir ve \u00f6deme s\u0131ras\u0131nda hesaplan\u0131r.",
		"orderTracking":                "Sipari\u015f Takibi",
		"orderTrackingContent":         "Sipari\u015finiz kargoya verildikten sonra takip numaras\u0131 i\u00e7eren bir e-posta alacaks\u0131n\u0131z. Bu numara ile kargo firmas\u0131n\u0131n web sitesinden paketinizi takip edebilirsiniz.",
		"shippingRestrictions":         "Kargo K\u0131s\u0131tlamalar\u0131",
		"shippingRestrictionsContent":  "Baz\u0131 \u00fcr\u00fcnlerin boyut, a\u011f\u0131rl\u0131k veya hedef d\u00fczenleme nedeniyle kargo k\u0131s\u0131tlamalar\u0131 olabilir. \u00d6deme s\u0131ras\u0131nda bilgilendirileceksiniz.",
		"shippingRates":                "Kargo \u00dccretleri",
		"shippingRatesAndDelivery":     "Kargo \u00dccretleri ve Teslimat S\u00fcreleri",
		"method":                       "Y\u00f6ntem",
		"deliveryTime":                 "Teslimat S\u00fcresi",
		"cost":                         "\u00dccret",
		"standardShipping":             "Standart Kargo",
		"standardShippingTime":         "5\u20137 i\u015f g\u00fcn\u00fc",
		"standardShippingCost":         "50\u20ba \u00fczeri sipari\u015flerde \u00fccretsiz",
		"expressShipping":              "H\u0131zl\u0131 Kargo",
		"expressShippingTime":          "2\u20133 i\u015f g\u00fcn\u00fc",
		"expressShippingCost":          "9,99\u20ba",
		"overnightShipping":            "Ertesi G\u00fcn Teslimat",
		"overnightShippingTime":        "1 i\u015f g\u00fcn\u00fc",
		"overnightShippingCost":        "19,99\u20ba",
		"returnsEligibility":           "Uygunluk Ko\u015fullar\u0131",
		"returnsEligibility1":          "\u00dcr\u00fcnler teslimat tarihinden itibaren 30 g\u00fcn i\u00e7inde iade edilmelidir.",
		"returnsEligibility2":          "\u00dcr\u00fcnler orijinal durumunda, giyilmemi\u015f, y\u0131kanmam\u0131\u015f ve etiketleri tak\u0131l\u0131 olmal\u0131d\u0131r.",
		"returnsEligibility3":          "\u00dcr\u00fcnler m\u00fcmk\u00fcnse orijinal ambalaj\u0131nda olmal\u0131d\u0131r.",
		"returnsEligibility4":          "\u0130ndirimli \u00fcr\u00fcnler ve hediye kartlar\u0131 iade edilemez.",
		"returnsEligibility5":          "Ki\u015fiye \u00f6zel veya \u0131smarlama \u00fcr\u00fcnler iade edilemez.",
		"returnsRefunds":               "Geri \u00d6demeler",
		"returnsRefundsContent":        "Geri \u00f6demeler orijinal \u00f6deme y\u00f6ntemine yap\u0131l\u0131r. \u0130adenizi ald\u0131ktan sonra geri \u00f6demenin hesab\u0131n\u0131zda g\u00f6r\u00fcnmesi i\u00e7in 5\u20137 i\u015f g\u00fcn\u00fc bekleyin.",
		"privacyIntro":                 "gizlili\u011finizi korumaya kararl\u0131d\u0131r. Bu Gizlilik Politikas\u0131, web sitemizi ziyaret etti\u011finizde ve ma\u011fazam\u0131zdan al\u0131\u015fveri\u015f yapt\u0131\u011f\u0131n\u0131zda bilgilerinizi nas\u0131l toplad\u0131\u011f\u0131m\u0131z\u0131, kulland\u0131\u011f\u0131m\u0131z\u0131 ve korudu\u011fumuzu a\u00e7\u0131klar.",
		"informationWeCollect":         "Toplad\u0131\u011f\u0131m\u0131z Bilgiler",
		"howWeUseInfo":                 "Bilgilerinizi Nas\u0131l Kullan\u0131yoruz",
		"privacyContact":               "Bize Ula\u015f\u0131n",
		"privacyContactContent":        "Bu Gizlilik Politikas\u0131 veya veri uygulamalar\u0131m\u0131z hakk\u0131nda sorular\u0131n\u0131z varsa, l\u00fctfen ileti\u015fim sayfam\u0131z arac\u0131l\u0131\u011f\u0131yla bize ula\u015f\u0131n.",
		"termsAcceptance":              "Ko\u015fullar\u0131n Kabul\u00fc",
		"termsAcceptanceContent":       "Bu web sitesine eri\u015ferek ve kullanarak, bu Kullan\u0131m Ko\u015fullar\u0131na ve ge\u00e7erli t\u00fcm yasa ve y\u00f6netmeliklere uymay\u0131 kabul etmi\u015f olursunuz.",
		"termsOrdersPayment":           "Sipari\u015fler ve \u00d6deme",
		"termsOrdersPaymentContent":    "Sipari\u015f vererek, verilen bilgilerin do\u011fru oldu\u011funu ve \u00f6deme y\u00f6ntemini kullanmaya yetkili oldu\u011funuzu beyan edersiniz.",
		"termsGoverningLaw":            "Y\u00fcr\u00fcrl\u00fckteki Hukuk",
		"termsGoverningLawContent":     "Bu Kullan\u0131m Ko\u015fullar\u0131, ge\u00e7erli yerel yasalara g\u00f6re y\u00f6netilecek ve yorumlanacakt\u0131r.",
		"termsChanges":                 "Ko\u015ful De\u011fi\u015fiklikleri",
		"termsChangesContent":          "Bu Kullan\u0131m Ko\u015fullar\u0131n\u0131 herhangi bir zamanda g\u00fcncelleme hakk\u0131m\u0131z\u0131 sakl\u0131 tutuyoruz. De\u011fi\u015fiklikler yay\u0131nland\u0131ktan hemen sonra ge\u00e7erli olacakt\u0131r.",
	},
}

// ecommerceExpressions holds JS expression-level replacements where word order
// changes between languages. The value is the complete JS expression to splice
// into the template. Templates use {{te.key}} for these.
var ecommerceExpressions = map[string]map[string]string{
	"en": {
		"awayFromFreeShipping": `formatPrice(shippingRemaining) + " away from free shipping"`,
	},
	"tr": {
		"awayFromFreeShipping": `"\u00dccretsiz kargo i\u00e7in " + formatPrice(shippingRemaining) + " daha"`,
	},
}

// LocalizeEcommerceTemplate replaces all {{t.key}} and {{te.key}} placeholders
// in a template string with the corresponding translated value for the given
// language. Falls back to English for missing keys.
// {{t.key}} = simple text replacement, {{te.key}} = full JS expression replacement.
func LocalizeEcommerceTemplate(template, lang string) string {
	strs, ok := ecommerceStrings[lang]
	if !ok {
		strs = ecommerceStrings["en"]
	}
	en := ecommerceStrings["en"]

	// Replace {{t.key}} text placeholders
	for key, val := range en {
		placeholder := "{{t." + key + "}}"
		localized := val
		if v, exists := strs[key]; exists {
			localized = v
		}
		template = strings.ReplaceAll(template, placeholder, localized)
	}

	// Replace {{te.key}} expression placeholders
	enExpr := ecommerceExpressions["en"]
	langExpr, ok := ecommerceExpressions[lang]
	if !ok {
		langExpr = enExpr
	}
	for key, val := range enExpr {
		placeholder := "{{te." + key + "}}"
		localized := val
		if v, exists := langExpr[key]; exists {
			localized = v
		}
		template = strings.ReplaceAll(template, placeholder, localized)
	}

	return template
}
