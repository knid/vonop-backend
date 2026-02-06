package controllers

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
)

// GetSEO returns SEO settings for a website.
func (c *Controller) GetSEO(user *models.User, websiteId string) (*responses.SEOResponse, error) {
	_, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	seo, err := c.DB.GetSEOByWebsiteId(websiteId)
	if err != nil {
		// No SEO yet — return empty
		return &responses.SEOResponse{
			WebsiteID: uuid.MustParse(websiteId),
			Keywords:  []string{},
		}, nil
	}

	return &responses.SEOResponse{
		ID:                seo.ID,
		WebsiteID:         seo.WebsiteID,
		MetaTitle:         seo.MetaTitle,
		MetaDescription:   seo.MetaDescription,
		OGImage:           seo.OGImage,
		OGTitle:           seo.OGTitle,
		OGDescription:     seo.OGDescription,
		FaviconURL:        seo.FaviconURL,
		GoogleAnalyticsID: seo.GoogleAnalyticsID,
		Keywords:          seo.Keywords,
		CreatedAt:         seo.CreatedAt,
		UpdatedAt:         seo.UpdatedAt,
	}, nil
}

// UpdateSEO creates or updates SEO settings (upsert).
func (c *Controller) UpdateSEO(user *models.User, websiteId string, req requests.UpdateSEORequest) (*responses.SEOResponse, error) {
	website, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	existing, err := c.DB.GetSEOByWebsiteId(websiteId)
	if err != nil {
		// Create new
		seo := models.WebsiteSEO{
			WebsiteID:         website.ID,
			MetaTitle:         req.MetaTitle,
			MetaDescription:   req.MetaDescription,
			OGImage:           req.OGImage,
			OGTitle:           req.OGTitle,
			OGDescription:     req.OGDescription,
			FaviconURL:        req.FaviconURL,
			GoogleAnalyticsID: req.GoogleAnalyticsID,
			Keywords:          req.Keywords,
		}
		created, err := c.DB.CreateSEO(&seo)
		if err != nil {
			return nil, err
		}
		return &responses.SEOResponse{
			ID:                created.ID,
			WebsiteID:         created.WebsiteID,
			MetaTitle:         created.MetaTitle,
			MetaDescription:   created.MetaDescription,
			OGImage:           created.OGImage,
			OGTitle:           created.OGTitle,
			OGDescription:     created.OGDescription,
			FaviconURL:        created.FaviconURL,
			GoogleAnalyticsID: created.GoogleAnalyticsID,
			Keywords:          created.Keywords,
			CreatedAt:         created.CreatedAt,
			UpdatedAt:         created.UpdatedAt,
		}, nil
	}

	// Update existing
	existing.MetaTitle = req.MetaTitle
	existing.MetaDescription = req.MetaDescription
	existing.OGImage = req.OGImage
	existing.OGTitle = req.OGTitle
	existing.OGDescription = req.OGDescription
	existing.FaviconURL = req.FaviconURL
	existing.GoogleAnalyticsID = req.GoogleAnalyticsID
	existing.Keywords = req.Keywords
	existing.UpdatedAt = time.Now()

	updated, err := c.DB.UpdateSEO(existing)
	if err != nil {
		return nil, err
	}

	return &responses.SEOResponse{
		ID:                updated.ID,
		WebsiteID:         updated.WebsiteID,
		MetaTitle:         updated.MetaTitle,
		MetaDescription:   updated.MetaDescription,
		OGImage:           updated.OGImage,
		OGTitle:           updated.OGTitle,
		OGDescription:     updated.OGDescription,
		FaviconURL:        updated.FaviconURL,
		GoogleAnalyticsID: updated.GoogleAnalyticsID,
		Keywords:          updated.Keywords,
		CreatedAt:         updated.CreatedAt,
		UpdatedAt:         updated.UpdatedAt,
	}, nil
}

// InjectSEOMetaTags inserts SEO meta tags before </head> in HTML content.
func InjectSEOMetaTags(html string, seo *models.WebsiteSEO) string {
	if seo == nil {
		return html
	}

	var tags strings.Builder

	if seo.MetaTitle != "" {
		tags.WriteString(fmt.Sprintf(`<title>%s</title>`, seo.MetaTitle))
		tags.WriteString("\n")
	}
	if seo.MetaDescription != "" {
		tags.WriteString(fmt.Sprintf(`<meta name="description" content="%s">`, seo.MetaDescription))
		tags.WriteString("\n")
	}
	if len(seo.Keywords) > 0 {
		tags.WriteString(fmt.Sprintf(`<meta name="keywords" content="%s">`, strings.Join(seo.Keywords, ", ")))
		tags.WriteString("\n")
	}
	if seo.OGTitle != "" {
		tags.WriteString(fmt.Sprintf(`<meta property="og:title" content="%s">`, seo.OGTitle))
		tags.WriteString("\n")
	}
	if seo.OGDescription != "" {
		tags.WriteString(fmt.Sprintf(`<meta property="og:description" content="%s">`, seo.OGDescription))
		tags.WriteString("\n")
	}
	if seo.OGImage != "" {
		tags.WriteString(fmt.Sprintf(`<meta property="og:image" content="%s">`, seo.OGImage))
		tags.WriteString("\n")
	}
	if seo.FaviconURL != "" {
		tags.WriteString(fmt.Sprintf(`<link rel="icon" href="%s">`, seo.FaviconURL))
		tags.WriteString("\n")
	}
	if seo.GoogleAnalyticsID != "" {
		tags.WriteString(fmt.Sprintf(`<script async src="https://www.googletagmanager.com/gtag/js?id=%s"></script>`, seo.GoogleAnalyticsID))
		tags.WriteString("\n")
		tags.WriteString(fmt.Sprintf(`<script>window.dataLayer=window.dataLayer||[];function gtag(){dataLayer.push(arguments);}gtag('js',new Date());gtag('config','%s');</script>`, seo.GoogleAnalyticsID))
		tags.WriteString("\n")
	}

	if tags.Len() == 0 {
		return html
	}

	return strings.Replace(html, "</head>", tags.String()+"</head>", 1)
}

// GenerateSitemap creates an XML sitemap from published HTML files.
func GenerateSitemap(baseURL string, filePaths []string) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	sb.WriteString("\n")
	sb.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
	sb.WriteString("\n")

	for _, path := range filePaths {
		if !strings.HasSuffix(path, ".html") {
			continue
		}
		urlPath := strings.TrimSuffix(path, "/index.html")
		if urlPath == "" {
			urlPath = "/"
		}
		sb.WriteString(fmt.Sprintf("  <url><loc>%s%s</loc></url>\n", baseURL, urlPath))
	}

	sb.WriteString("</urlset>")
	return sb.String()
}

// GenerateRobotsTxt creates a standard robots.txt file.
func GenerateRobotsTxt(sitemapURL string) string {
	return fmt.Sprintf("User-agent: *\nAllow: /\nSitemap: %s\n", sitemapURL)
}
