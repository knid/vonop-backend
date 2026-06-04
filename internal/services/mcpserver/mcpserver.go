package mcpserver

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Service struct {
	controller *controllers.Controller
	handler    http.Handler
}

type authContext struct {
	user           *models.User
	apiKey         *models.APIKey
	organizationID uuid.UUID
	scopes         string
}

type authContextKey struct{}

type emptyArgs struct{}

type listAppsArgs struct {
	ProjectType string `json:"project_type,omitempty" jsonschema:"Optional project type filter: app, ecommerce, or mobile_app_native."`
	Limit       int    `json:"limit,omitempty" jsonschema:"Optional maximum number of apps to return. Defaults to 50 and caps at 100."`
}

type getAppArgs struct {
	AppID string `json:"app_id" jsonschema:"The Wemob app project ID."`
}

type appSummary struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Slug           string         `json:"slug"`
	ProjectType    string         `json:"project_type"`
	Status         string         `json:"status"`
	OrganizationID string         `json:"organization_id"`
	ProductionURL  string         `json:"production_url,omitempty"`
	Prompt         string         `json:"prompt,omitempty"`
	Settings       map[string]any `json:"settings,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type whoAmIOutput struct {
	UserID         string `json:"user_id"`
	Email          string `json:"email"`
	APIKeyID       string `json:"api_key_id"`
	OrganizationID string `json:"organization_id"`
	Scopes         string `json:"scopes"`
}

type listAppsOutput struct {
	OrganizationID string       `json:"organization_id"`
	Apps           []appSummary `json:"apps"`
}

type getAppOutput struct {
	App appSummary `json:"app"`
}

func New(controller *controllers.Controller) *Service {
	s := &Service{controller: controller}
	s.handler = mcp.NewStreamableHTTPHandler(s.serverForRequest, &mcp.StreamableHTTPOptions{
		JSONResponse: true,
		Stateless:    true,
	})
	return s
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/mcp/health" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	auth, err := s.authenticate(r)
	if err != nil {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="https://mcp.wemob.io/.well-known/oauth-protected-resource"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	s.handler.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authContextKey{}, auth)))
}

func (s *Service) serverForRequest(r *http.Request) *mcp.Server {
	auth, ok := r.Context().Value(authContextKey{}).(*authContext)
	if !ok || auth == nil {
		return nil
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "wemob",
		Version: "1.0.0",
	}, nil)

	closedWorld := false
	annotations := &mcp.ToolAnnotations{
		ReadOnlyHint:  true,
		OpenWorldHint: &closedWorld,
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "whoami",
		Title:       "Wemob Connection Info",
		Description: "Return the authenticated Wemob user, API key, organization, and scopes for this MCP connection.",
		Annotations: annotations,
	}, func(ctx context.Context, req *mcp.CallToolRequest, args emptyArgs) (*mcp.CallToolResult, whoAmIOutput, error) {
		if err := requireAnyScope(auth.scopes, "projects:read", "apps:read"); err != nil {
			return nil, whoAmIOutput{}, err
		}
		apiKeyID := ""
		if auth.apiKey != nil {
			apiKeyID = auth.apiKey.ID.String()
		}
		return nil, whoAmIOutput{
			UserID:         auth.user.ID.String(),
			Email:          auth.user.Email,
			APIKeyID:       apiKeyID,
			OrganizationID: auth.organizationID.String(),
			Scopes:         auth.scopes,
		}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_apps",
		Title:       "List Wemob Apps",
		Description: "List Wemob apps visible to the authenticated API key's organization. Optionally filter by project type.",
		Annotations: annotations,
	}, func(ctx context.Context, req *mcp.CallToolRequest, args listAppsArgs) (*mcp.CallToolResult, listAppsOutput, error) {
		if err := requireAnyScope(auth.scopes, "projects:read", "apps:read"); err != nil {
			return nil, listAppsOutput{}, err
		}

		types, err := projectTypes(args.ProjectType)
		if err != nil {
			return nil, listAppsOutput{}, err
		}

		var projects []models.AppProject
		orgID := auth.organizationID.String()
		if len(types) == 0 {
			projects, err = s.controller.DB.GetAppProjectsByOrgID(orgID)
		} else {
			projects, err = s.controller.DB.GetAppProjectsByOrgIDAndTypes(orgID, types)
		}
		if err != nil {
			return nil, listAppsOutput{}, err
		}

		limit := args.Limit
		if limit <= 0 {
			limit = 50
		}
		if limit > 100 {
			limit = 100
		}
		if len(projects) > limit {
			projects = projects[:limit]
		}

		apps := make([]appSummary, 0, len(projects))
		for _, project := range projects {
			apps = append(apps, summarizeApp(project, false))
		}

		return nil, listAppsOutput{
			OrganizationID: orgID,
			Apps:           apps,
		}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_app",
		Title:       "Get Wemob App",
		Description: "Return details for one Wemob app project if it belongs to the authenticated API key's organization.",
		Annotations: annotations,
	}, func(ctx context.Context, req *mcp.CallToolRequest, args getAppArgs) (*mcp.CallToolResult, getAppOutput, error) {
		if err := requireAnyScope(auth.scopes, "projects:read", "apps:read"); err != nil {
			return nil, getAppOutput{}, err
		}
		if strings.TrimSpace(args.AppID) == "" {
			return nil, getAppOutput{}, errors.New("app_id is required")
		}

		project, err := s.controller.GetAppProject(args.AppID, auth.user)
		if err != nil {
			return nil, getAppOutput{}, err
		}
		if project.OrganizationID != auth.organizationID {
			return nil, getAppOutput{}, errors.New("app not found")
		}

		return nil, getAppOutput{App: summarizeApp(*project, true)}, nil
	})

	return server
}

func (s *Service) authenticate(r *http.Request) (*authContext, error) {
	bearerToken, err := utils.ExtractTokenFromHeader(r)
	if err != nil {
		return nil, err
	}

	if strings.HasPrefix(bearerToken, "wemob_") {
		user, apiKey, err := s.controller.AuthenticateAPIKey(bearerToken)
		if err != nil {
			return nil, err
		}
		if user == nil || apiKey == nil || apiKey.OrganizationID == uuid.Nil {
			return nil, errors.New("invalid API key context")
		}
		return &authContext{
			user:           user,
			apiKey:         apiKey,
			organizationID: apiKey.OrganizationID,
			scopes:         apiKey.Scopes,
		}, nil
	}

	token, err := s.controller.DB.GetTokenByToken(bearerToken)
	if err != nil {
		return nil, err
	}
	if token.ExpiredAt.Before(time.Now()) {
		return nil, errors.New("token is expired")
	}
	user, err := s.controller.DB.GetUserById(token.UserID.String())
	if err != nil {
		return nil, err
	}
	orgID, err := s.controller.RequireOrgIDForUser(user, nil)
	if err != nil {
		return nil, err
	}
	return &authContext{
		user:           user,
		organizationID: orgID,
		scopes:         "projects:read apps:read",
	}, nil
}

func requireAnyScope(scopeString string, scopes ...string) error {
	granted := map[string]bool{}
	for _, scope := range strings.FieldsFunc(scopeString, func(r rune) bool {
		return r == ',' || r == ' '
	}) {
		granted[strings.TrimSpace(scope)] = true
	}
	for _, scope := range scopes {
		if granted[scope] {
			return nil
		}
	}
	return errors.New("API key does not have a required read scope")
}

func projectTypes(projectType string) ([]string, error) {
	projectType = strings.TrimSpace(projectType)
	if projectType == "" {
		return nil, nil
	}
	switch projectType {
	case models.ProjectTypeApp, models.ProjectTypeEcommerce, models.ProjectTypeMobileAppNative:
		return []string{projectType}, nil
	default:
		return nil, errors.New("project_type must be app, ecommerce, or mobile_app_native")
	}
}

func summarizeApp(project models.AppProject, includePrompt bool) appSummary {
	url := ""
	if project.Slug != "" {
		url = "https://" + project.Slug + ".wemob.app"
	}

	out := appSummary{
		ID:             project.ID.String(),
		Name:           project.Name,
		Slug:           project.Slug,
		ProjectType:    project.ProjectType,
		Status:         project.Status,
		OrganizationID: project.OrganizationID.String(),
		ProductionURL:  url,
		Settings:       project.Settings,
		CreatedAt:      project.CreatedAt,
		UpdatedAt:      project.UpdatedAt,
	}
	if includePrompt {
		out.Prompt = project.Prompt
	}
	return out
}
