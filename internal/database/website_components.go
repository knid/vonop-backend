package database

import (
	"context"
	"log/slog"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetActiveComponents() ([]models.WebsiteComponent, error) {
	var components []models.WebsiteComponent
	err := db.DB.NewSelect().Model(&components).Where("is_active = ?", true).Scan(context.Background())
	return components, err
}

func (db *Database) GetComponentsByCategory(category string) ([]models.WebsiteComponent, error) {
	var components []models.WebsiteComponent
	err := db.DB.NewSelect().Model(&components).Where("is_active = ? AND category = ?", true, category).Scan(context.Background())
	return components, err
}

func (db *Database) GetComponentById(id string) (*models.WebsiteComponent, error) {
	var component models.WebsiteComponent
	err := db.DB.NewSelect().Model(&component).Where("id = ?", id).Scan(context.Background())
	return &component, err
}

func (db *Database) CreateComponent(c *models.WebsiteComponent) (*models.WebsiteComponent, error) {
	_, err := db.DB.NewInsert().Model(c).Exec(context.Background())
	return c, err
}

func (db *Database) GetComponentCount() (int, error) {
	count, err := db.DB.NewSelect().Model((*models.WebsiteComponent)(nil)).Count(context.Background())
	return count, err
}

func (db *Database) SeedComponents() {
	count, err := db.GetComponentCount()
	if err != nil || count > 0 {
		return
	}

	components := []models.WebsiteComponent{
		{
			Name:     "Hero Centered",
			Category: "hero",
			Tags:     []string{"hero", "centered", "cta"},
			HTMLTemplate: `<section class="hero-section bg-gradient-to-br from-primary to-secondary text-white py-24 px-6 text-center">
  <div class="max-w-4xl mx-auto">
    <h1 class="text-5xl md:text-6xl font-bold mb-6">Your Amazing Headline Here</h1>
    <p class="text-xl md:text-2xl mb-8 opacity-90">A compelling subheadline that describes your value proposition and engages your audience.</p>
    <div class="flex flex-col sm:flex-row gap-4 justify-center">
      <a href="#" class="bg-white text-primary px-8 py-4 rounded-lg font-semibold text-lg hover:bg-opacity-90 transition">Get Started</a>
      <a href="#" class="border-2 border-white px-8 py-4 rounded-lg font-semibold text-lg hover:bg-white hover:text-primary transition">Learn More</a>
    </div>
  </div>
</section>`,
		},
		{
			Name:     "Hero Split",
			Category: "hero",
			Tags:     []string{"hero", "split", "image"},
			HTMLTemplate: `<section class="hero-split py-20 px-6">
  <div class="max-w-7xl mx-auto grid md:grid-cols-2 gap-12 items-center">
    <div>
      <h1 class="text-4xl md:text-5xl font-bold mb-6" style="color: var(--text)">Transform Your Business Today</h1>
      <p class="text-lg mb-8 opacity-80" style="color: var(--text)">Discover how our solution can help you achieve your goals faster and more efficiently than ever before.</p>
      <a href="#" class="inline-block px-8 py-4 rounded-lg font-semibold text-white text-lg transition hover:opacity-90" style="background: var(--primary)">Start Free Trial</a>
    </div>
    <div>
      <img src="https://placehold.co/600x400" alt="Hero image" class="rounded-2xl shadow-2xl w-full">
    </div>
  </div>
</section>`,
		},
		{
			Name:     "Pricing Three Tier",
			Category: "pricing",
			Tags:     []string{"pricing", "cards", "tiers"},
			HTMLTemplate: `<section class="pricing-section py-20 px-6" style="background: var(--background)">
  <div class="max-w-7xl mx-auto">
    <h2 class="text-3xl md:text-4xl font-bold text-center mb-4" style="color: var(--text)">Simple, Transparent Pricing</h2>
    <p class="text-center text-lg mb-12 opacity-70" style="color: var(--text)">Choose the plan that works best for you</p>
    <div class="grid md:grid-cols-3 gap-8">
      <div class="bg-white rounded-2xl p-8 shadow-lg border border-gray-100">
        <h3 class="text-xl font-bold mb-2">Starter</h3>
        <p class="text-4xl font-bold mb-6">$9<span class="text-lg font-normal opacity-60">/mo</span></p>
        <ul class="space-y-3 mb-8">
          <li class="flex items-center gap-2"><span class="text-green-500">&#10003;</span> 5 Projects</li>
          <li class="flex items-center gap-2"><span class="text-green-500">&#10003;</span> Basic Analytics</li>
          <li class="flex items-center gap-2"><span class="text-green-500">&#10003;</span> Email Support</li>
        </ul>
        <a href="#" class="block text-center py-3 rounded-lg border-2 font-semibold transition hover:opacity-80" style="border-color: var(--primary); color: var(--primary)">Get Started</a>
      </div>
      <div class="bg-white rounded-2xl p-8 shadow-xl border-2 relative" style="border-color: var(--primary)">
        <span class="absolute -top-4 left-1/2 -translate-x-1/2 px-4 py-1 rounded-full text-white text-sm font-semibold" style="background: var(--primary)">Popular</span>
        <h3 class="text-xl font-bold mb-2">Pro</h3>
        <p class="text-4xl font-bold mb-6">$29<span class="text-lg font-normal opacity-60">/mo</span></p>
        <ul class="space-y-3 mb-8">
          <li class="flex items-center gap-2"><span class="text-green-500">&#10003;</span> Unlimited Projects</li>
          <li class="flex items-center gap-2"><span class="text-green-500">&#10003;</span> Advanced Analytics</li>
          <li class="flex items-center gap-2"><span class="text-green-500">&#10003;</span> Priority Support</li>
          <li class="flex items-center gap-2"><span class="text-green-500">&#10003;</span> Custom Domain</li>
        </ul>
        <a href="#" class="block text-center py-3 rounded-lg text-white font-semibold transition hover:opacity-90" style="background: var(--primary)">Get Started</a>
      </div>
      <div class="bg-white rounded-2xl p-8 shadow-lg border border-gray-100">
        <h3 class="text-xl font-bold mb-2">Enterprise</h3>
        <p class="text-4xl font-bold mb-6">$99<span class="text-lg font-normal opacity-60">/mo</span></p>
        <ul class="space-y-3 mb-8">
          <li class="flex items-center gap-2"><span class="text-green-500">&#10003;</span> Everything in Pro</li>
          <li class="flex items-center gap-2"><span class="text-green-500">&#10003;</span> SSO & SAML</li>
          <li class="flex items-center gap-2"><span class="text-green-500">&#10003;</span> Dedicated Manager</li>
          <li class="flex items-center gap-2"><span class="text-green-500">&#10003;</span> SLA Guarantee</li>
        </ul>
        <a href="#" class="block text-center py-3 rounded-lg border-2 font-semibold transition hover:opacity-80" style="border-color: var(--primary); color: var(--primary)">Contact Sales</a>
      </div>
    </div>
  </div>
</section>`,
		},
		{
			Name:     "FAQ Accordion",
			Category: "faq",
			Tags:     []string{"faq", "accordion", "questions"},
			HTMLTemplate: `<section class="faq-section py-20 px-6" style="background: var(--background)">
  <div class="max-w-3xl mx-auto">
    <h2 class="text-3xl md:text-4xl font-bold text-center mb-12" style="color: var(--text)">Frequently Asked Questions</h2>
    <div class="space-y-4">
      <details class="bg-white rounded-xl p-6 shadow-sm border border-gray-100 group" open>
        <summary class="font-semibold text-lg cursor-pointer list-none flex justify-between items-center">What is your refund policy?<span class="transition group-open:rotate-180">&#9660;</span></summary>
        <p class="mt-4 opacity-70">We offer a 30-day money-back guarantee. If you're not satisfied, contact us for a full refund.</p>
      </details>
      <details class="bg-white rounded-xl p-6 shadow-sm border border-gray-100 group">
        <summary class="font-semibold text-lg cursor-pointer list-none flex justify-between items-center">How do I get started?<span class="transition group-open:rotate-180">&#9660;</span></summary>
        <p class="mt-4 opacity-70">Simply sign up for a free account, choose your plan, and start building right away.</p>
      </details>
      <details class="bg-white rounded-xl p-6 shadow-sm border border-gray-100 group">
        <summary class="font-semibold text-lg cursor-pointer list-none flex justify-between items-center">Do you offer custom plans?<span class="transition group-open:rotate-180">&#9660;</span></summary>
        <p class="mt-4 opacity-70">Yes! Contact our sales team for custom enterprise solutions tailored to your needs.</p>
      </details>
    </div>
  </div>
</section>`,
		},
		{
			Name:     "Testimonials Carousel",
			Category: "testimonials",
			Tags:     []string{"testimonials", "reviews", "social-proof"},
			HTMLTemplate: `<section class="testimonials-section py-20 px-6" style="background: var(--background)">
  <div class="max-w-7xl mx-auto">
    <h2 class="text-3xl md:text-4xl font-bold text-center mb-12" style="color: var(--text)">What Our Customers Say</h2>
    <div class="grid md:grid-cols-3 gap-8">
      <div class="bg-white rounded-2xl p-8 shadow-lg">
        <div class="flex items-center gap-1 mb-4 text-yellow-400">&#9733;&#9733;&#9733;&#9733;&#9733;</div>
        <p class="mb-6 opacity-80">"This product completely transformed how we work. The results have been incredible and the support team is amazing."</p>
        <div class="flex items-center gap-3">
          <img src="https://placehold.co/48x48" alt="Avatar" class="w-12 h-12 rounded-full">
          <div><p class="font-semibold">Sarah Johnson</p><p class="text-sm opacity-60">CEO, TechCorp</p></div>
        </div>
      </div>
      <div class="bg-white rounded-2xl p-8 shadow-lg">
        <div class="flex items-center gap-1 mb-4 text-yellow-400">&#9733;&#9733;&#9733;&#9733;&#9733;</div>
        <p class="mb-6 opacity-80">"I've tried many solutions, but this one stands out. It's intuitive, powerful, and delivers real results."</p>
        <div class="flex items-center gap-3">
          <img src="https://placehold.co/48x48" alt="Avatar" class="w-12 h-12 rounded-full">
          <div><p class="font-semibold">Michael Chen</p><p class="text-sm opacity-60">CTO, StartupXYZ</p></div>
        </div>
      </div>
      <div class="bg-white rounded-2xl p-8 shadow-lg">
        <div class="flex items-center gap-1 mb-4 text-yellow-400">&#9733;&#9733;&#9733;&#9733;&#9733;</div>
        <p class="mb-6 opacity-80">"Outstanding quality and excellent customer service. Highly recommend to anyone looking to scale."</p>
        <div class="flex items-center gap-3">
          <img src="https://placehold.co/48x48" alt="Avatar" class="w-12 h-12 rounded-full">
          <div><p class="font-semibold">Emily Davis</p><p class="text-sm opacity-60">Founder, GrowthLab</p></div>
        </div>
      </div>
    </div>
  </div>
</section>`,
		},
		{
			Name:     "CTA Banner",
			Category: "cta",
			Tags:     []string{"cta", "banner", "action"},
			HTMLTemplate: `<section class="cta-section py-20 px-6 text-white text-center" style="background: linear-gradient(135deg, var(--primary), var(--secondary))">
  <div class="max-w-3xl mx-auto">
    <h2 class="text-3xl md:text-4xl font-bold mb-4">Ready to Get Started?</h2>
    <p class="text-xl mb-8 opacity-90">Join thousands of satisfied customers and take your business to the next level.</p>
    <a href="#" class="inline-block bg-white px-8 py-4 rounded-lg font-semibold text-lg transition hover:bg-opacity-90" style="color: var(--primary)">Start Your Free Trial</a>
  </div>
</section>`,
		},
		{
			Name:     "Footer",
			Category: "footer",
			Tags:     []string{"footer", "links", "copyright"},
			HTMLTemplate: `<footer class="py-16 px-6" style="background: var(--text); color: var(--background)">
  <div class="max-w-7xl mx-auto grid md:grid-cols-4 gap-8">
    <div>
      <h3 class="text-xl font-bold mb-4">Company</h3>
      <p class="opacity-70">Building the future, one step at a time.</p>
    </div>
    <div>
      <h4 class="font-semibold mb-4">Product</h4>
      <ul class="space-y-2 opacity-70">
        <li><a href="#" class="hover:opacity-100 transition">Features</a></li>
        <li><a href="#" class="hover:opacity-100 transition">Pricing</a></li>
        <li><a href="#" class="hover:opacity-100 transition">Integrations</a></li>
      </ul>
    </div>
    <div>
      <h4 class="font-semibold mb-4">Company</h4>
      <ul class="space-y-2 opacity-70">
        <li><a href="#" class="hover:opacity-100 transition">About</a></li>
        <li><a href="#" class="hover:opacity-100 transition">Blog</a></li>
        <li><a href="#" class="hover:opacity-100 transition">Careers</a></li>
      </ul>
    </div>
    <div>
      <h4 class="font-semibold mb-4">Support</h4>
      <ul class="space-y-2 opacity-70">
        <li><a href="#" class="hover:opacity-100 transition">Help Center</a></li>
        <li><a href="#" class="hover:opacity-100 transition">Contact</a></li>
        <li><a href="#" class="hover:opacity-100 transition">Privacy Policy</a></li>
      </ul>
    </div>
  </div>
  <div class="max-w-7xl mx-auto mt-12 pt-8 border-t border-white border-opacity-20 text-center opacity-60">
    <p>&copy; 2026 Company Name. All rights reserved.</p>
  </div>
</footer>`,
		},
		{
			Name:     "Contact Form",
			Category: "contact",
			Tags:     []string{"contact", "form", "email"},
			HTMLTemplate: `<section class="contact-section py-20 px-6" style="background: var(--background)">
  <div class="max-w-3xl mx-auto">
    <h2 class="text-3xl md:text-4xl font-bold text-center mb-4" style="color: var(--text)">Get in Touch</h2>
    <p class="text-center text-lg mb-12 opacity-70" style="color: var(--text)">Have a question? We'd love to hear from you.</p>
    <form class="space-y-6">
      <div class="grid md:grid-cols-2 gap-6">
        <div>
          <label class="block text-sm font-semibold mb-2" style="color: var(--text)">First Name</label>
          <input type="text" class="w-full px-4 py-3 rounded-lg border border-gray-300 focus:outline-none focus:ring-2" style="focus:ring-color: var(--primary)" placeholder="John">
        </div>
        <div>
          <label class="block text-sm font-semibold mb-2" style="color: var(--text)">Last Name</label>
          <input type="text" class="w-full px-4 py-3 rounded-lg border border-gray-300 focus:outline-none focus:ring-2" placeholder="Doe">
        </div>
      </div>
      <div>
        <label class="block text-sm font-semibold mb-2" style="color: var(--text)">Email</label>
        <input type="email" class="w-full px-4 py-3 rounded-lg border border-gray-300 focus:outline-none focus:ring-2" placeholder="john@example.com">
      </div>
      <div>
        <label class="block text-sm font-semibold mb-2" style="color: var(--text)">Message</label>
        <textarea rows="5" class="w-full px-4 py-3 rounded-lg border border-gray-300 focus:outline-none focus:ring-2" placeholder="Your message..."></textarea>
      </div>
      <button type="submit" class="w-full py-4 rounded-lg text-white font-semibold text-lg transition hover:opacity-90" style="background: var(--primary)">Send Message</button>
    </form>
  </div>
</section>`,
		},
		{
			Name:     "Features Grid",
			Category: "features",
			Tags:     []string{"features", "grid", "icons"},
			HTMLTemplate: `<section class="features-section py-20 px-6" style="background: var(--background)">
  <div class="max-w-7xl mx-auto">
    <h2 class="text-3xl md:text-4xl font-bold text-center mb-4" style="color: var(--text)">Powerful Features</h2>
    <p class="text-center text-lg mb-12 opacity-70" style="color: var(--text)">Everything you need to succeed</p>
    <div class="grid md:grid-cols-3 gap-8">
      <div class="bg-white rounded-2xl p-8 shadow-sm border border-gray-100 text-center">
        <div class="w-16 h-16 rounded-2xl flex items-center justify-center mx-auto mb-6 text-white text-2xl" style="background: var(--primary)">&#9889;</div>
        <h3 class="text-xl font-bold mb-3">Lightning Fast</h3>
        <p class="opacity-70">Optimized for speed and performance. Load times under 100ms guaranteed.</p>
      </div>
      <div class="bg-white rounded-2xl p-8 shadow-sm border border-gray-100 text-center">
        <div class="w-16 h-16 rounded-2xl flex items-center justify-center mx-auto mb-6 text-white text-2xl" style="background: var(--primary)">&#128274;</div>
        <h3 class="text-xl font-bold mb-3">Secure by Default</h3>
        <p class="opacity-70">Enterprise-grade security with end-to-end encryption and SOC 2 compliance.</p>
      </div>
      <div class="bg-white rounded-2xl p-8 shadow-sm border border-gray-100 text-center">
        <div class="w-16 h-16 rounded-2xl flex items-center justify-center mx-auto mb-6 text-white text-2xl" style="background: var(--primary)">&#128640;</div>
        <h3 class="text-xl font-bold mb-3">Easy to Scale</h3>
        <p class="opacity-70">Grow without limits. Automatic scaling handles millions of requests seamlessly.</p>
      </div>
    </div>
  </div>
</section>`,
		},
		{
			Name:     "Team Section",
			Category: "team",
			Tags:     []string{"team", "people", "about"},
			HTMLTemplate: `<section class="team-section py-20 px-6" style="background: var(--background)">
  <div class="max-w-7xl mx-auto">
    <h2 class="text-3xl md:text-4xl font-bold text-center mb-4" style="color: var(--text)">Meet Our Team</h2>
    <p class="text-center text-lg mb-12 opacity-70" style="color: var(--text)">The people behind the magic</p>
    <div class="grid md:grid-cols-4 gap-8">
      <div class="text-center">
        <img src="https://placehold.co/200x200" alt="Team member" class="w-32 h-32 rounded-full mx-auto mb-4 object-cover">
        <h3 class="text-lg font-bold" style="color: var(--text)">Alex Thompson</h3>
        <p class="opacity-60" style="color: var(--text)">CEO & Founder</p>
      </div>
      <div class="text-center">
        <img src="https://placehold.co/200x200" alt="Team member" class="w-32 h-32 rounded-full mx-auto mb-4 object-cover">
        <h3 class="text-lg font-bold" style="color: var(--text)">Maria Garcia</h3>
        <p class="opacity-60" style="color: var(--text)">CTO</p>
      </div>
      <div class="text-center">
        <img src="https://placehold.co/200x200" alt="Team member" class="w-32 h-32 rounded-full mx-auto mb-4 object-cover">
        <h3 class="text-lg font-bold" style="color: var(--text)">James Wilson</h3>
        <p class="opacity-60" style="color: var(--text)">Head of Design</p>
      </div>
      <div class="text-center">
        <img src="https://placehold.co/200x200" alt="Team member" class="w-32 h-32 rounded-full mx-auto mb-4 object-cover">
        <h3 class="text-lg font-bold" style="color: var(--text)">Lisa Park</h3>
        <p class="opacity-60" style="color: var(--text)">Lead Developer</p>
      </div>
    </div>
  </div>
</section>`,
		},
	}

	for _, comp := range components {
		_, err := db.CreateComponent(&comp)
		if err != nil {
			slog.Error("failed to seed component", "name", comp.Name, "error", err)
		}
	}

	slog.Info("seeded website components", "count", len(components))
}
