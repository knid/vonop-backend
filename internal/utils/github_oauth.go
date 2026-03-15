package utils

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OAuthState is the payload embedded in the HMAC-signed state param.
type OAuthState struct {
	UserID  string `json:"u"`
	Expires int64  `json:"e"`
}

// GenerateOAuthState creates an HMAC-signed, base64-encoded state string.
func GenerateOAuthState(userID, secret string) string {
	state := OAuthState{
		UserID:  userID,
		Expires: time.Now().Add(10 * time.Minute).Unix(),
	}
	payload, _ := json.Marshal(state)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	sig := mac.Sum(nil)
	return base64.URLEncoding.EncodeToString(payload) + "." + base64.URLEncoding.EncodeToString(sig)
}

// VerifyOAuthState verifies the HMAC signature and expiry. Returns the user ID.
func VerifyOAuthState(stateStr, secret string) (string, error) {
	parts := strings.SplitN(stateStr, ".", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid state format")
	}
	payload, err := base64.URLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", fmt.Errorf("invalid state encoding")
	}
	sig, err := base64.URLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("invalid signature encoding")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return "", fmt.Errorf("invalid state signature")
	}
	var state OAuthState
	if err := json.Unmarshal(payload, &state); err != nil {
		return "", fmt.Errorf("invalid state payload")
	}
	if time.Now().Unix() > state.Expires {
		return "", fmt.Errorf("state expired")
	}
	return state.UserID, nil
}

// GitHubTokenResponse is the response from GitHub's OAuth token exchange.
type GitHubTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
}

// ExchangeGitHubCode exchanges an authorization code for an access token.
func ExchangeGitHubCode(clientID, clientSecret, code, redirectURI string) (*GitHubTokenResponse, error) {
	data := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
	}
	req, _ := http.NewRequest("POST", "https://github.com/login/oauth/access_token", strings.NewReader(data.Encode()))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github token exchange: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp GitHubTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("github token decode: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("github token exchange failed: no access_token in response")
	}
	return &tokenResp, nil
}

// GitHubUser represents the relevant fields from GET /user.
type GitHubUser struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
}

// FetchGitHubUser fetches the authenticated user's profile.
func FetchGitHubUser(accessToken string) (*GitHubUser, error) {
	req, _ := http.NewRequest("GET", "https://api.github.com/user", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github fetch user: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github fetch user: status %d: %s", resp.StatusCode, body)
	}

	var user GitHubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("github decode user: %w", err)
	}
	return &user, nil
}

// GitHubCreateRepoRequest is the request body for creating a GitHub repo.
type GitHubCreateRepoRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Private     bool   `json:"private"`
	AutoInit    bool   `json:"auto_init"`
}

// GitHubRepoResponse represents the relevant fields from the create repo response.
type GitHubRepoResponse struct {
	FullName string `json:"full_name"`
	HTMLURL  string `json:"html_url"`
	CloneURL string `json:"clone_url"`
}

// CreateGitHubRepo creates a new repository for the authenticated user.
func CreateGitHubRepo(accessToken, name, description string, private bool) (*GitHubRepoResponse, error) {
	body := GitHubCreateRepoRequest{
		Name:        name,
		Description: description,
		Private:     private,
		AutoInit:    false,
	}
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "https://api.github.com/user/repos", strings.NewReader(string(jsonBody)))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github create repo: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == 422 {
		return nil, fmt.Errorf("GITHUB_REPO_EXISTS")
	}
	if resp.StatusCode != 201 {
		return nil, fmt.Errorf("github create repo: status %d: %s", resp.StatusCode, respBody)
	}

	var repoResp GitHubRepoResponse
	if err := json.Unmarshal(respBody, &repoResp); err != nil {
		return nil, fmt.Errorf("github decode repo: %w", err)
	}
	return &repoResp, nil
}
