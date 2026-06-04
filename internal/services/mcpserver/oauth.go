package mcpserver

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

const (
	issuerURL        = "https://mcp.wemob.io"
	resourceURL      = "https://mcp.wemob.io"
	defaultMCPScopes = "projects:read apps:read"
)

type authCode struct {
	Code          string
	UserID        string
	ClientID      string
	RedirectURI   string
	Resource      string
	Scope         string
	CodeChallenge string
	ExpiresAt     time.Time
}

type codeStore struct {
	mu    sync.Mutex
	codes map[string]authCode
}

var oauthCodes = &codeStore{codes: map[string]authCode{}}

func (s *Service) ServeProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":              resourceURL,
		"authorization_servers": []string{issuerURL},
		"scopes_supported":      []string{"projects:read", "apps:read"},
		"bearer_methods_supported": []string{
			"header",
		},
	})
}

func (s *Service) ServeAuthorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                issuerURL,
		"authorization_endpoint":                issuerURL + "/oauth/authorize",
		"token_endpoint":                        issuerURL + "/oauth/token",
		"registration_endpoint":                 issuerURL + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported":                      []string{"projects:read", "apps:read"},
		"resource_documentation":                "https://wemob.io",
	})
}

func (s *Service) RegisterOAuthClient(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	clientID, err := utils.RandomHex(16)
	if err != nil {
		http.Error(w, "failed to register client", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  "wemob-mcp-" + clientID,
		"client_id_issued_at":        time.Now().Unix(),
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code"},
		"response_types":             []string{"code"},
	})
}

func (s *Service) Authorize(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if err := validateAuthorizeParams(r.URL.Query()); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		renderAuthorizeForm(w, r.URL.Query(), "")
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		if err := validateAuthorizeParams(r.PostForm); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		user, err := s.authenticatePassword(r.PostForm.Get("email"), r.PostForm.Get("password"))
		if err != nil {
			renderAuthorizeForm(w, r.PostForm, "Invalid email or password.")
			return
		}
		code, err := s.createAuthorizationCode(user, r.PostForm)
		if err != nil {
			http.Error(w, "failed to create authorization code", http.StatusInternalServerError)
			return
		}
		redirectURI, _ := url.Parse(r.PostForm.Get("redirect_uri"))
		q := redirectURI.Query()
		q.Set("code", code)
		if state := r.PostForm.Get("state"); state != "" {
			q.Set("state", state)
		}
		redirectURI.RawQuery = q.Encode()
		http.Redirect(w, r, redirectURI.String(), http.StatusFound)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Service) Token(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		oauthError(w, "invalid_request", "invalid form")
		return
	}
	if r.PostForm.Get("grant_type") != "authorization_code" {
		oauthError(w, "unsupported_grant_type", "only authorization_code is supported")
		return
	}
	code := oauthCodes.take(r.PostForm.Get("code"))
	if code == nil {
		oauthError(w, "invalid_grant", "authorization code is invalid or expired")
		return
	}
	if code.ClientID != r.PostForm.Get("client_id") || code.RedirectURI != r.PostForm.Get("redirect_uri") {
		oauthError(w, "invalid_grant", "client_id or redirect_uri does not match")
		return
	}
	if resource := r.PostForm.Get("resource"); resource != "" && resource != code.Resource {
		oauthError(w, "invalid_target", "resource does not match")
		return
	}
	if !verifyS256(r.PostForm.Get("code_verifier"), code.CodeChallenge) {
		oauthError(w, "invalid_grant", "code_verifier does not match")
		return
	}

	user, err := s.controller.DB.GetUserById(code.UserID)
	if err != nil {
		oauthError(w, "invalid_grant", "user no longer exists")
		return
	}
	tokenStr, err := utils.RandomHex(32)
	if err != nil {
		oauthError(w, "server_error", "failed to issue access token")
		return
	}
	expiresAt := time.Now().AddDate(0, 0, 14)
	if _, err := s.controller.DB.CreateToken(&models.Token{
		UserID:    user.ID,
		String:    tokenStr,
		ExpiredAt: expiresAt,
	}); err != nil {
		oauthError(w, "server_error", "failed to persist access token")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": tokenStr,
		"token_type":   "Bearer",
		"expires_in":   int(time.Until(expiresAt).Seconds()),
		"scope":        code.Scope,
	})
}

func (s *Service) authenticatePassword(email, password string) (*models.User, error) {
	req := requests.UserLoginRequest{Email: email, Password: password}
	if req.Email == "" || req.Password == "" {
		return nil, controllers.ErrInvalidCredentials
	}
	user, err := s.controller.DB.GetUserByEmail(req.Email)
	if err != nil {
		return nil, controllers.ErrInvalidCredentials
	}
	if !utils.CheckPassword(req.Password, user.Password) {
		return nil, controllers.ErrInvalidCredentials
	}
	return user, nil
}

func (s *Service) createAuthorizationCode(user *models.User, form url.Values) (string, error) {
	code, err := utils.RandomHex(32)
	if err != nil {
		return "", err
	}
	scope := normalizeScope(form.Get("scope"))
	if scope == "" {
		scope = defaultMCPScopes
	}
	resource := form.Get("resource")
	if resource == "" {
		resource = resourceURL
	}
	oauthCodes.put(authCode{
		Code:          code,
		UserID:        user.ID.String(),
		ClientID:      form.Get("client_id"),
		RedirectURI:   form.Get("redirect_uri"),
		Resource:      resource,
		Scope:         scope,
		CodeChallenge: form.Get("code_challenge"),
		ExpiresAt:     time.Now().Add(10 * time.Minute),
	})
	return code, nil
}

func validateAuthorizeParams(v url.Values) error {
	if v.Get("response_type") != "code" {
		return errors.New("response_type must be code")
	}
	if v.Get("client_id") == "" {
		return errors.New("client_id is required")
	}
	if v.Get("redirect_uri") == "" {
		return errors.New("redirect_uri is required")
	}
	redirectURI, err := url.Parse(v.Get("redirect_uri"))
	if err != nil || redirectURI.Scheme == "" || redirectURI.Host == "" {
		return errors.New("redirect_uri must be an absolute URL")
	}
	if v.Get("code_challenge") == "" {
		return errors.New("code_challenge is required")
	}
	if v.Get("code_challenge_method") != "S256" {
		return errors.New("code_challenge_method must be S256")
	}
	if resource := v.Get("resource"); resource != "" && resource != resourceURL {
		return errors.New("resource must be https://mcp.wemob.io")
	}
	return nil
}

func normalizeScope(scope string) string {
	if strings.TrimSpace(scope) == "" {
		return ""
	}
	allowed := map[string]bool{"projects:read": true, "apps:read": true}
	var out []string
	for _, part := range strings.Fields(scope) {
		if allowed[part] {
			out = append(out, part)
		}
	}
	return strings.Join(out, " ")
}

func verifyS256(verifier, challenge string) bool {
	if verifier == "" || challenge == "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	encoded := base64.RawURLEncoding.EncodeToString(sum[:])
	return encoded == challenge
}

func renderAuthorizeForm(w http.ResponseWriter, values url.Values, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	hidden := []string{"response_type", "client_id", "redirect_uri", "state", "scope", "resource", "code_challenge", "code_challenge_method"}
	var fields strings.Builder
	for _, name := range hidden {
		fields.WriteString(fmt.Sprintf(`<input type="hidden" name="%s" value="%s">`, html.EscapeString(name), html.EscapeString(values.Get(name))))
	}
	errHTML := ""
	if message != "" {
		errHTML = `<p style="color:#b42318">` + html.EscapeString(message) + `</p>`
	}
	fmt.Fprintf(w, `<!doctype html>
<html><head><meta name="viewport" content="width=device-width, initial-scale=1"><title>Authorize Wemob MCP</title></head>
<body style="font-family:-apple-system,BlinkMacSystemFont,Segoe UI,sans-serif;max-width:420px;margin:48px auto;padding:0 20px;color:#111827">
<h1 style="font-size:24px">Connect Wemob</h1>
<p>Sign in to allow this AI client to read your Wemob apps through MCP.</p>
%s
<form method="post" action="/oauth/authorize">
%s
<label>Email<br><input name="email" type="email" required autocomplete="email" style="width:100%%;padding:10px;margin:6px 0 14px"></label><br>
<label>Password<br><input name="password" type="password" required autocomplete="current-password" style="width:100%%;padding:10px;margin:6px 0 18px"></label><br>
<button type="submit" style="width:100%%;padding:12px;background:#111827;color:white;border:0;border-radius:6px">Authorize</button>
</form>
</body></html>`, errHTML, fields.String())
}

func (s *codeStore) put(code authCode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.codes[code.Code] = code
}

func (s *codeStore) take(code string) *authCode {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.codes[code]
	if !ok || stored.ExpiresAt.Before(time.Now()) {
		delete(s.codes, code)
		return nil
	}
	delete(s.codes, code)
	return &stored
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func oauthError(w http.ResponseWriter, code, description string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{
		"error":             code,
		"error_description": description,
	})
}
