package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/mcpclient"
	"golang.org/x/oauth2"
)

const mcpOAuthMetadataBodyLimit = 1 << 20

type mcpProtectedResourceMetadata struct {
	Resource              string   `json:"resource"`
	AuthorizationServers  []string `json:"authorization_servers"`
	ScopesSupported       []string `json:"scopes_supported"`
	ResourceName          string   `json:"resource_name"`
	ResourceDocumentation string   `json:"resource_documentation"`
}

type mcpAuthorizationServerMetadata struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RegistrationEndpoint  string   `json:"registration_endpoint"`
	ScopesSupported       []string `json:"scopes_supported"`
}

type mcpOAuthDiscovery struct {
	Resource                     string
	ProtectedResourceMetadataURL string
	AuthorizationServer          string
	AuthorizationEndpoint        string
	TokenEndpoint                string
	RegistrationEndpoint         string
	ScopesSupported              []string
	ResourceName                 string
	ResourceDocumentation        string
}

type mcpOAuthClientRegistration struct {
	ClientID                string `json:"client_id"`
	ClientSecret            string `json:"client_secret"`
	RegistrationAccessToken string `json:"registration_access_token"`
	ClientIDIssuedAt        int64  `json:"client_id_issued_at"`
	ClientSecretExpiresAt   int64  `json:"client_secret_expires_at"`
}

type mcpOAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

func (c *Controller) prepareMCPOAuthConfig(ctx context.Context, connection *models.MCPConnection) (*oauth2.Config, error) {
	if connection.Config == nil {
		connection.Config = map[string]any{}
	}
	changed := false
	if _, ok := connection.Config["scopes"]; !ok {
		if template := resolveMCPTemplate(connection.TemplateID); template != nil && len(template.Scopes) > 0 {
			connection.Config["scopes"] = append([]string(nil), template.Scopes...)
			changed = true
		}
	}

	needsDiscovery := strings.TrimSpace(stringFromAny(connection.Config["authorization_endpoint"])) == "" ||
		strings.TrimSpace(stringFromAny(connection.Config["token_endpoint"])) == "" ||
		strings.TrimSpace(stringFromAny(connection.Config["client_id"])) == ""

	template := resolveMCPTemplate(connection.TemplateID)
	if needsDiscovery && (template == nil || template.SupportsOAuthDiscovery || template.ServerURL != "") {
		discovery, err := c.discoverMCPOAuth(ctx, connection.ServerURL)
		if err != nil {
			if strings.TrimSpace(stringFromAny(connection.Config["authorization_endpoint"])) == "" ||
				strings.TrimSpace(stringFromAny(connection.Config["token_endpoint"])) == "" {
				return nil, err
			}
			connection.Config["oauth_discovery_error"] = err.Error()
			changed = true
		} else {
			applyMCPOAuthDiscovery(connection.Config, discovery)
			changed = true
		}
	}

	if strings.TrimSpace(stringFromAny(connection.Config["client_id"])) == "" {
		registrationEndpoint := strings.TrimSpace(stringFromAny(connection.Config["registration_endpoint"]))
		if registrationEndpoint == "" {
			return nil, fmt.Errorf("oauth client_id is required and the MCP authorization server did not advertise dynamic client registration")
		}
		registration, err := c.registerMCPOAuthClient(ctx, connection, registrationEndpoint)
		if err != nil {
			return nil, err
		}
		connection.Config["client_id"] = registration.ClientID
		connection.Config["oauth_client_registration"] = "dynamic"
		connection.Config["oauth_client_registered_at"] = time.Now().Unix()
		if registration.ClientIDIssuedAt > 0 {
			connection.Config["client_id_issued_at"] = registration.ClientIDIssuedAt
		}
		if registration.ClientSecretExpiresAt > 0 {
			connection.Config["client_secret_expires_at"] = registration.ClientSecretExpiresAt
		}
		creds, err := c.decryptMCPCredentials(connection)
		if err != nil {
			return nil, err
		}
		if registration.ClientSecret != "" {
			creds["client_secret"] = registration.ClientSecret
		}
		if registration.RegistrationAccessToken != "" {
			creds["registration_access_token"] = registration.RegistrationAccessToken
		}
		enc, err := c.encryptMCPCredentials(creds)
		if err != nil {
			return nil, err
		}
		connection.CredentialData = enc
		connection.CredentialsEncryptedV1 = true
		changed = true
	}

	if changed {
		if _, err := c.DB.UpdateMCPConnection(connection); err != nil {
			return nil, err
		}
	}
	return c.oauthConfigForConnection(connection)
}

func (c *Controller) discoverMCPOAuth(ctx context.Context, serverURL string) (*mcpOAuthDiscovery, error) {
	resource, err := mcpCanonicalResourceURI(serverURL)
	if err != nil {
		return nil, err
	}
	metadataURL, err := mcpProtectedResourceMetadataURL(resource)
	if err != nil {
		return nil, err
	}
	prm, usedMetadataURL, err := fetchMCPProtectedResourceMetadata(ctx, metadataURL)
	if err != nil {
		challengeURL, challengeErr := discoverMCPProtectedResourceMetadataURLFromChallenge(ctx, serverURL)
		if challengeErr != nil {
			return nil, err
		}
		prm, usedMetadataURL, err = fetchMCPProtectedResourceMetadata(ctx, challengeURL)
		if err != nil {
			return nil, err
		}
	}
	if prm.Resource != "" && !sameMCPResource(prm.Resource, resource) {
		return nil, fmt.Errorf("oauth protected resource metadata resource mismatch")
	}
	authServer := firstNonEmptyString(prm.AuthorizationServers)
	if authServer == "" {
		return nil, fmt.Errorf("oauth protected resource metadata did not include authorization_servers")
	}
	asMeta, err := fetchMCPAuthorizationServerMetadata(ctx, authServer)
	if err != nil {
		return nil, err
	}
	if asMeta.AuthorizationEndpoint == "" || asMeta.TokenEndpoint == "" {
		return nil, fmt.Errorf("oauth authorization server metadata is missing authorization_endpoint or token_endpoint")
	}
	if err := validateMCPRemoteURL("authorization_endpoint", asMeta.AuthorizationEndpoint); err != nil {
		return nil, err
	}
	if err := validateMCPRemoteURL("token_endpoint", asMeta.TokenEndpoint); err != nil {
		return nil, err
	}
	if asMeta.RegistrationEndpoint != "" {
		if err := validateMCPRemoteURL("registration_endpoint", asMeta.RegistrationEndpoint); err != nil {
			return nil, err
		}
	}
	scopesSupported := append([]string(nil), prm.ScopesSupported...)
	if len(scopesSupported) == 0 {
		scopesSupported = append([]string(nil), asMeta.ScopesSupported...)
	}
	return &mcpOAuthDiscovery{
		Resource:                     resource,
		ProtectedResourceMetadataURL: usedMetadataURL,
		AuthorizationServer:          authServer,
		AuthorizationEndpoint:        asMeta.AuthorizationEndpoint,
		TokenEndpoint:                asMeta.TokenEndpoint,
		RegistrationEndpoint:         asMeta.RegistrationEndpoint,
		ScopesSupported:              scopesSupported,
		ResourceName:                 prm.ResourceName,
		ResourceDocumentation:        prm.ResourceDocumentation,
	}, nil
}

func applyMCPOAuthDiscovery(config map[string]any, discovery *mcpOAuthDiscovery) {
	if discovery == nil {
		return
	}
	config["oauth_resource"] = discovery.Resource
	config["oauth_resource_metadata_url"] = discovery.ProtectedResourceMetadataURL
	config["oauth_authorization_server"] = discovery.AuthorizationServer
	config["authorization_endpoint"] = discovery.AuthorizationEndpoint
	config["token_endpoint"] = discovery.TokenEndpoint
	if discovery.RegistrationEndpoint != "" {
		config["registration_endpoint"] = discovery.RegistrationEndpoint
	}
	if len(discovery.ScopesSupported) > 0 {
		config["oauth_scopes_supported"] = append([]string(nil), discovery.ScopesSupported...)
	}
	if discovery.ResourceName != "" {
		config["oauth_resource_name"] = discovery.ResourceName
	}
	if discovery.ResourceDocumentation != "" {
		config["oauth_resource_documentation"] = discovery.ResourceDocumentation
	}
	delete(config, "oauth_discovery_error")
}

func (c *Controller) registerMCPOAuthClient(ctx context.Context, connection *models.MCPConnection, registrationEndpoint string) (*mcpOAuthClientRegistration, error) {
	if err := validateMCPRemoteURL("registration_endpoint", registrationEndpoint); err != nil {
		return nil, err
	}
	scopes := stringSliceFromAny(connection.Config["scopes"])
	payload := map[string]any{
		"client_name":                "Wemob " + connection.Name,
		"redirect_uris":              []string{c.mcpOAuthRedirectURL()},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
		"application_type":           "web",
	}
	if len(scopes) > 0 {
		payload["scope"] = strings.Join(scopes, " ")
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, registrationEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := mcpclient.NewSafeHTTPClient(nil, 30*time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, mcpOAuthMetadataBodyLimit))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("oauth dynamic client registration failed with status %d", resp.StatusCode)
	}
	var registration mcpOAuthClientRegistration
	if err := json.Unmarshal(data, &registration); err != nil {
		return nil, fmt.Errorf("invalid oauth dynamic client registration response: %w", err)
	}
	if strings.TrimSpace(registration.ClientID) == "" {
		return nil, fmt.Errorf("oauth dynamic client registration response missing client_id")
	}
	return &registration, nil
}

func refreshMCPOAuthToken(ctx context.Context, cfg *oauth2.Config, refreshToken, resource string) (*oauth2.Token, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", cfg.ClientID)
	if cfg.ClientSecret != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}
	if resource != "" {
		form.Set("resource", resource)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.Endpoint.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := mcpclient.NewSafeHTTPClient(nil, 30*time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, mcpOAuthMetadataBodyLimit))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("oauth refresh failed with status %d", resp.StatusCode)
	}
	var tokenResponse mcpOAuthTokenResponse
	if err := json.Unmarshal(data, &tokenResponse); err != nil {
		return nil, fmt.Errorf("invalid oauth refresh response: %w", err)
	}
	if strings.TrimSpace(tokenResponse.AccessToken) == "" {
		return nil, fmt.Errorf("oauth refresh response missing access_token")
	}
	if tokenResponse.RefreshToken == "" {
		tokenResponse.RefreshToken = refreshToken
	}
	tok := &oauth2.Token{
		AccessToken:  tokenResponse.AccessToken,
		TokenType:    tokenResponse.TokenType,
		RefreshToken: tokenResponse.RefreshToken,
	}
	if tokenResponse.ExpiresIn > 0 {
		tok.Expiry = time.Now().Add(time.Duration(tokenResponse.ExpiresIn) * time.Second)
	}
	return tok, nil
}

func fetchMCPProtectedResourceMetadata(ctx context.Context, metadataURL string) (*mcpProtectedResourceMetadata, string, error) {
	if err := validateMCPRemoteURL("resource_metadata", metadataURL); err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := mcpclient.NewSafeHTTPClient(nil, 30*time.Second).Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, mcpOAuthMetadataBodyLimit))
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("oauth protected resource metadata returned status %d", resp.StatusCode)
	}
	var metadata mcpProtectedResourceMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, "", fmt.Errorf("invalid oauth protected resource metadata: %w", err)
	}
	return &metadata, resp.Request.URL.String(), nil
}

func discoverMCPProtectedResourceMetadataURLFromChallenge(ctx context.Context, serverURL string) (string, error) {
	if err := validateMCPRemoteURL("server_url", serverURL); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := mcpclient.NewSafeHTTPClient(nil, 15*time.Second).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	for _, header := range resp.Header.Values("WWW-Authenticate") {
		if metadataURL := parseWWWAuthenticateParam(header, "resource_metadata"); metadataURL != "" {
			return metadataURL, nil
		}
	}
	return "", fmt.Errorf("oauth protected resource metadata was not advertised")
}

func fetchMCPAuthorizationServerMetadata(ctx context.Context, issuer string) (*mcpAuthorizationServerMetadata, error) {
	urls, err := oauthAuthorizationServerMetadataURLs(issuer)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, metadataURL := range urls {
		if err := validateMCPRemoteURL("authorization_server_metadata", metadataURL); err != nil {
			lastErr = err
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		resp, err := mcpclient.NewSafeHTTPClient(nil, 30*time.Second).Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, mcpOAuthMetadataBodyLimit))
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("oauth authorization server metadata returned status %d", resp.StatusCode)
			continue
		}
		var metadata mcpAuthorizationServerMetadata
		if err := json.Unmarshal(data, &metadata); err != nil {
			return nil, fmt.Errorf("invalid oauth authorization server metadata: %w", err)
		}
		return &metadata, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("oauth authorization server metadata not found")
	}
	return nil, lastErr
}

func mcpCanonicalResourceURI(raw string) (string, error) {
	if err := validateMCPRemoteURL("server_url", raw); err != nil {
		return "", err
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.User = nil
	u.Fragment = ""
	if u.Path == "/" {
		u.Path = ""
	}
	return u.String(), nil
}

func mcpProtectedResourceMetadataURL(resource string) (string, error) {
	u, err := url.Parse(resource)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("resource must be a valid URL")
	}
	path := u.EscapedPath()
	if path == "/" {
		path = ""
	}
	u.Path = "/.well-known/oauth-protected-resource" + path
	u.RawPath = ""
	u.Fragment = ""
	return u.String(), nil
}

func oauthAuthorizationServerMetadataURLs(issuer string) ([]string, error) {
	if err := validateMCPRemoteURL("authorization_server", issuer); err != nil {
		return nil, err
	}
	u, err := url.Parse(strings.TrimSpace(issuer))
	if err != nil {
		return nil, err
	}
	path := u.EscapedPath()
	if path == "/" {
		path = ""
	}
	u.Path = "/.well-known/oauth-authorization-server" + path
	u.RawPath = ""
	u.Fragment = ""
	urls := []string{u.String()}

	u.Path = "/.well-known/openid-configuration" + path
	u.RawPath = ""
	urls = appendUniqueString(urls, u.String())

	if path != "" {
		u.Path = strings.TrimRight(path, "/") + "/.well-known/openid-configuration"
		u.RawPath = ""
		urls = appendUniqueString(urls, u.String())
	}
	return urls, nil
}

func sameMCPResource(a, b string) bool {
	ca, errA := mcpCanonicalResourceURI(a)
	cb, errB := mcpCanonicalResourceURI(b)
	return errA == nil && errB == nil && ca == cb
}

func parseWWWAuthenticateParam(header, key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	rest := strings.TrimSpace(header)
	for rest != "" {
		idx := strings.IndexByte(rest, '=')
		if idx < 0 {
			return ""
		}
		nameStart := strings.LastIndexAny(rest[:idx], " \t,")
		name := strings.ToLower(strings.TrimSpace(rest[nameStart+1 : idx]))
		valueStart := idx + 1
		value := ""
		if valueStart < len(rest) && rest[valueStart] == '"' {
			pos := valueStart + 1
			var b strings.Builder
			for pos < len(rest) {
				ch := rest[pos]
				if ch == '\\' && pos+1 < len(rest) {
					b.WriteByte(rest[pos+1])
					pos += 2
					continue
				}
				if ch == '"' {
					pos++
					break
				}
				b.WriteByte(ch)
				pos++
			}
			value = b.String()
			rest = strings.TrimLeft(rest[pos:], " \t,")
		} else {
			pos := valueStart
			for pos < len(rest) && rest[pos] != ',' && rest[pos] != ' ' && rest[pos] != '\t' {
				pos++
			}
			value = strings.TrimSpace(rest[valueStart:pos])
			rest = strings.TrimLeft(rest[pos:], " \t,")
		}
		if name == key {
			return value
		}
	}
	return ""
}

func firstNonEmptyString(values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func stringFromAny(v any) string {
	s, _ := v.(string)
	return s
}
