package codemagic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

// Workflow IDs matching codemagic.yaml
const (
	WorkflowAndroidBuild  = "android-build"
	WorkflowIOSBuild      = "ios-build"
	WorkflowIOSSign       = "ios-sign"
	WorkflowAndroidDeploy = "android-deploy"
	WorkflowIOSDeploy     = "ios-deploy"
)

type Service struct {
	APIToken string
	AppID    string
	Branch   string
	// apiBaseURL and httpClient may be overridden in tests; zero values fall
	// back to the production defaults (https://api.codemagic.io and the
	// package-level httpClient with 30s timeout).
	apiBaseURL string
	client     *http.Client
}

func NewService(apiToken, appID string) *Service {
	return &Service{
		APIToken: apiToken,
		AppID:    appID,
		Branch:   "main",
	}
}

// baseURL returns the Codemagic API base URL, falling back to production.
func (s *Service) baseURL() string {
	if s.apiBaseURL != "" {
		return s.apiBaseURL
	}
	return "https://api.codemagic.io"
}

// SetTestEndpoint overrides the API base URL for testing. Use only from
// integration tests; production callers should construct via NewService.
//
// Exists so integration tests in other packages (e.g. controllers) can
// redirect Codemagic API calls to an httptest.Server without exposing the
// unexported apiBaseURL field directly.
func (s *Service) SetTestEndpoint(baseURL string) {
	s.apiBaseURL = baseURL
}

// httpClientOrDefault returns s.client if set, else the package-level httpClient.
func (s *Service) httpClientOrDefault() *http.Client {
	if s.client != nil {
		return s.client
	}
	return httpClient
}

type triggerRequest struct {
	AppID      string              `json:"appId"`
	WorkflowID string             `json:"workflowId"`
	Branch     string              `json:"branch"`
	Env        *triggerEnvironment `json:"environment,omitempty"`
}

type triggerEnvironment struct {
	Variables map[string]string `json:"variables,omitempty"`
}

type triggerResponse struct {
	BuildID string `json:"buildId"`
}

// TriggerBuild starts a Codemagic build and returns the build ID.
// Retries up to 3 times on 5xx and network errors with ~2s linear backoff.
func (s *Service) TriggerBuild(workflowID string, variables map[string]string) (string, error) {
	body := triggerRequest{
		AppID:      s.AppID,
		WorkflowID: workflowID,
		Branch:     s.Branch,
		Env: &triggerEnvironment{
			Variables: variables,
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("codemagic: marshal: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		req, err := http.NewRequest("POST", s.baseURL()+"/builds", bytes.NewReader(jsonBody))
		if err != nil {
			return "", fmt.Errorf("codemagic: create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-auth-token", s.APIToken)

		resp, err := s.httpClientOrDefault().Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(2 * time.Second)
			continue
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return "", fmt.Errorf("codemagic: read response: %w", err)
		}

		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("codemagic: API returned %d: %s", resp.StatusCode, string(respBody))
			time.Sleep(2 * time.Second)
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			slog.Error("codemagic: trigger failed",
				"status", resp.StatusCode,
				"body", string(respBody),
				"workflow", workflowID,
			)
			return "", fmt.Errorf("codemagic: API returned %d: %s", resp.StatusCode, string(respBody))
		}

		var result triggerResponse
		if err := json.Unmarshal(respBody, &result); err != nil {
			return "", fmt.Errorf("codemagic: unmarshal response: %w", err)
		}

		slog.Info("codemagic: build triggered",
			"build_id", result.BuildID,
			"workflow", workflowID,
		)

		return result.BuildID, nil
	}
	return "", fmt.Errorf("codemagic: trigger failed after retries: %w", lastErr)
}

// TriggerAndroidBuild triggers an Android build workflow.
func (s *Service) TriggerAndroidBuild(params AndroidBuildParams) (string, error) {
	vars := map[string]string{
		"CM_PROJECT_ID":           params.ProjectID,
		"CM_BUILD_ID":             params.BuildID,
		"CM_TASK_ID":              params.TaskID,
		"CM_VERSION":              fmt.Sprintf("%d", params.Version),
		"CM_TITLE":                params.Title,
		"CM_URL":                  params.URL,
		"CM_ICON_URL":             params.IconURL,
		"CM_SPLASH_COLOR":         params.SplashColor,
		"CM_ONESIGNAL_APP_ID":     params.OneSignalAppID,
		"CM_PACKAGE_NAME":         params.PackageName,
		"CM_REMOVE_BRANDING":      boolStr(params.RemoveBranding),
		"CM_PERMISSIONS":          strings.Join(params.Permissions, ","),
		"CM_URL_SCHEME":           params.URLScheme,
		"CM_UNIVERSAL_LINK_DOMAIN": params.UniversalLinkDomain,
	}
	return s.TriggerBuild(WorkflowAndroidBuild, vars)
}

// TriggerIOSBuild triggers an iOS build workflow.
func (s *Service) TriggerIOSBuild(params IOSBuildParams) (string, error) {
	vars := map[string]string{
		"CM_PROJECT_ID":           params.ProjectID,
		"CM_BUILD_ID":             params.BuildID,
		"CM_TASK_ID":              params.TaskID,
		"CM_VERSION":              fmt.Sprintf("%d", params.Version),
		"CM_TITLE":                params.Title,
		"CM_URL":                  params.URL,
		"CM_ICON_URL":             params.IconURL,
		"CM_SPLASH_COLOR":         params.SplashColor,
		"CM_ONESIGNAL_APP_ID":     params.OneSignalAppID,
		"CM_PACKAGE_NAME":         params.PackageName,
		"CM_REMOVE_BRANDING":      boolStr(params.RemoveBranding),
		"CM_PERMISSIONS":          strings.Join(params.Permissions, ","),
		"CM_URL_SCHEME":           params.URLScheme,
		"CM_UNIVERSAL_LINK_DOMAIN": params.UniversalLinkDomain,
	}
	return s.TriggerBuild(WorkflowIOSBuild, vars)
}

// TriggerIOSSign triggers an iOS signing workflow.
func (s *Service) TriggerIOSSign(params IOSSignParams) (string, error) {
	vars := map[string]string{
		"CM_BUILD_ID":       params.BuildID,
		"CM_TASK_ID":        params.TaskID,
		"CM_PACKAGE_NAME":   params.PackageName,
		"CM_TEAM_ID":        params.TeamID,
		"CM_KEY_ID":         params.KeyID,
		"CM_ISSUER_ID":      params.IssuerID,
		"CM_P8_KEY_S3_PATH": params.P8KeyS3Path,
	}
	return s.TriggerBuild(WorkflowIOSSign, vars)
}

// TriggerAndroidDeploy triggers an Android deploy workflow.
func (s *Service) TriggerAndroidDeploy(params AndroidDeployParams) (string, error) {
	vars := map[string]string{
		"CM_BUILD_ID":             params.BuildID,
		"CM_DEPLOYMENT_ID":        params.DeploymentID,
		"CM_PACKAGE_NAME":         params.PackageName,
		"CM_VERSION":              fmt.Sprintf("%d", params.Version),
		"CM_TITLE":                params.Title,
		"CM_DESCRIPTION":          params.Description,
		"CM_SCREENSHOTS":          strings.Join(params.Screenshots, ","),
		"CM_ICON_PATH":            params.IconPath,
		"CM_LOCALE":               params.Locale,
		"CM_SERVICE_ACCOUNT_S3":   params.ServiceAccountS3Path,
		"CM_NEW_APP":              boolStr(params.NewApp),
		"CM_CHANGED_BUILD":        boolStr(params.ChangedBuild),
		"CM_TITLE_CHANGED":        boolStr(params.TitleChanged),
		"CM_DESC_CHANGED":         boolStr(params.DescChanged),
		"CM_SCREENSHOTS_CHANGED":  boolStr(params.ScreenshotsChanged),
		"CM_ICON_CHANGED":         boolStr(params.IconChanged),
	}
	return s.TriggerBuild(WorkflowAndroidDeploy, vars)
}

// TriggerIOSDeploy triggers an iOS deploy workflow.
func (s *Service) TriggerIOSDeploy(params IOSDeployParams) (string, error) {
	vars := map[string]string{
		"CM_BUILD_ID":             params.BuildID,
		"CM_DEPLOYMENT_ID":        params.DeploymentID,
		"CM_PACKAGE_NAME":         params.PackageName,
		"CM_TEAM_ID":              params.TeamID,
		"CM_KEY_ID":               params.KeyID,
		"CM_ISSUER_ID":            params.IssuerID,
		"CM_P8_KEY_S3_PATH":       params.P8KeyS3Path,
		"CM_TITLE":                params.Title,
		"CM_DESCRIPTION":          params.Description,
		"CM_SCREENSHOTS":          strings.Join(params.Screenshots, ","),
		"CM_ICON":                 params.Icon,
		"CM_LOCALE":               params.Locale,
		"CM_NEW_APP":              boolStr(params.NewApp),
		"CM_CHANGED_BUILD":        boolStr(params.ChangedBuild),
		"CM_TITLE_CHANGED":        boolStr(params.TitleChanged),
		"CM_DESC_CHANGED":         boolStr(params.DescChanged),
		"CM_SCREENSHOTS_CHANGED":  boolStr(params.ScreenshotsChanged),
	}
	if params.RolloutTrack != "" {
		vars["CM_ROLLOUT_TRACK"] = params.RolloutTrack
	}
	if params.ReleaseNotes != "" {
		vars["CM_RELEASE_NOTES"] = params.ReleaseNotes
	}
	return s.TriggerBuild(WorkflowIOSDeploy, vars)
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
