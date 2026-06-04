package controllers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/agent"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/jobstream"
	"github.com/knid/wemob/internal/services/mcpclient"
	"github.com/knid/wemob/internal/utils"
	"golang.org/x/oauth2"
)

type mcpTemplate struct {
	ID                string
	Name              string
	Description       string
	Category          string
	AuthType          string
	ServerURL         string
	RequiresServerURL bool
	Scopes            []string
}

var mcpTemplates = []mcpTemplate{
	{
		ID:                "email_remote_oauth",
		Name:              "Email MCP",
		Description:       "Remote MCP server for project email workflows.",
		Category:          "communication",
		AuthType:          models.MCPAuthTypeOAuth,
		RequiresServerURL: true,
		Scopes:            []string{"email.read", "email.send"},
	},
	{
		ID:                "calendar_remote_oauth",
		Name:              "Calendar MCP",
		Description:       "Remote MCP server for scheduling and calendar workflows.",
		Category:          "productivity",
		AuthType:          models.MCPAuthTypeOAuth,
		RequiresServerURL: true,
		Scopes:            []string{"calendar.read", "calendar.write"},
	},
}

func (c *Controller) ListMCPTemplates() []responses.MCPTemplateResponse {
	resp := make([]responses.MCPTemplateResponse, 0, len(mcpTemplates))
	for _, t := range mcpTemplates {
		resp = append(resp, responses.MCPTemplateResponse{
			ID:                t.ID,
			Name:              t.Name,
			Description:       t.Description,
			Category:          t.Category,
			AuthType:          t.AuthType,
			ServerURL:         t.ServerURL,
			RequiresServerURL: t.RequiresServerURL,
			Scopes:            append([]string(nil), t.Scopes...),
		})
	}
	return resp
}

func (c *Controller) CreateMCPConnection(project *models.AppProject, req requests.CreateMCPConnectionRequest) (*models.MCPConnection, error) {
	if project == nil {
		return nil, fmt.Errorf("project is required")
	}
	template := resolveMCPTemplate(req.TemplateID)
	name := strings.TrimSpace(req.Name)
	if name == "" && template != nil {
		name = template.Name
	}
	if name == "" {
		name = "Custom MCP"
	}
	authType := normalizeMCPAuthType(req.AuthType)
	if authType == "" && template != nil {
		authType = template.AuthType
	}
	if authType == "" {
		authType = models.MCPAuthTypeAPIKey
	}
	serverURL := strings.TrimSpace(req.ServerURL)
	if serverURL == "" && template != nil {
		serverURL = template.ServerURL
	}
	if serverURL == "" {
		return nil, fmt.Errorf("server_url is required")
	}
	if err := validateHTTPURL(serverURL); err != nil {
		return nil, err
	}

	status := models.MCPConnectionStatusPendingAuth
	var credentialData []byte
	config := sanitizeMCPConfig(req.Config)
	if config == nil {
		config = map[string]any{}
	}
	if authType == models.MCPAuthTypeNone || authType == models.MCPAuthTypeAPIKey {
		status = models.MCPConnectionStatusConnected
		creds := req.Credentials
		if creds == nil {
			creds = map[string]any{}
		}
		var err error
		credentialData, err = c.encryptMCPCredentials(creds)
		if err != nil {
			return nil, err
		}
	} else {
		creds := map[string]any{}
		if secret, _ := req.Config["client_secret"].(string); secret != "" {
			creds["client_secret"] = secret
		}
		var err error
		credentialData, err = c.encryptMCPCredentials(creds)
		if err != nil {
			return nil, err
		}
	}

	connection := &models.MCPConnection{
		AppProjectID:           project.ID,
		Name:                   name,
		Slug:                   utils.SlugifyName(name),
		TemplateID:             req.TemplateID,
		AuthType:               authType,
		ServerURL:              serverURL,
		Status:                 status,
		Config:                 config,
		CredentialData:         credentialData,
		CredentialsEncryptedV1: true,
	}
	return c.DB.CreateMCPConnection(connection)
}

func (c *Controller) StartMCPOAuth(project *models.AppProject, req requests.StartMCPOAuthRequest) (*responses.MCPOAuthStartResponse, error) {
	var connection *models.MCPConnection
	var err error
	if req.ConnectionID != "" {
		connection, err = c.DB.GetMCPConnectionByID(req.ConnectionID)
		if err != nil || connection.AppProjectID != project.ID {
			return nil, fmt.Errorf("connection not found")
		}
	} else {
		connection, err = c.CreateMCPConnection(project, requests.CreateMCPConnectionRequest{
			TemplateID: req.TemplateID,
			Name:       req.Name,
			ServerURL:  req.ServerURL,
			AuthType:   models.MCPAuthTypeOAuth,
			Config:     req.Config,
		})
		if err != nil {
			return nil, err
		}
	}
	if connection.AuthType != models.MCPAuthTypeOAuth {
		return nil, fmt.Errorf("connection does not use oauth")
	}

	cfg, err := c.oauthConfigForConnection(connection)
	if err != nil {
		return nil, err
	}
	state := randomOAuthString(32)
	verifier := randomOAuthString(48)
	challenge := oauthS256Challenge(verifier)
	if connection.Config == nil {
		connection.Config = map[string]any{}
	}
	connection.Config["oauth_state"] = state
	connection.Config["oauth_code_verifier"] = verifier
	if req.RedirectURL != "" {
		connection.Config["return_url"] = req.RedirectURL
	}
	connection.Status = models.MCPConnectionStatusPendingAuth
	if _, err := c.DB.UpdateMCPConnection(connection); err != nil {
		return nil, err
	}

	authURL := cfg.AuthCodeURL(
		state,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
	resp := c.mcpConnectionResponse(*connection, nil)
	return &responses.MCPOAuthStartResponse{Connection: resp, AuthURL: authURL}, nil
}

func (c *Controller) CompleteMCPOAuthCallback(ctx context.Context, state, code string) (string, error) {
	if strings.TrimSpace(state) == "" || strings.TrimSpace(code) == "" {
		return "", fmt.Errorf("state and code are required")
	}
	connections, err := c.findMCPConnectionByOAuthState(state)
	if err != nil {
		return "", err
	}
	connection := connections
	verifier, _ := connection.Config["oauth_code_verifier"].(string)
	if verifier == "" {
		return "", fmt.Errorf("oauth state is missing verifier")
	}
	cfg, err := c.oauthConfigForConnection(connection)
	if err != nil {
		return "", err
	}
	token, err := cfg.Exchange(ctx, code, oauth2.SetAuthURLParam("code_verifier", verifier))
	if err != nil {
		connection.Status = models.MCPConnectionStatusError
		connection.LastError = "oauth token exchange failed"
		_, _ = c.DB.UpdateMCPConnection(connection)
		return "", err
	}

	creds := map[string]any{
		"access_token":  token.AccessToken,
		"token_type":    token.TokenType,
		"refresh_token": token.RefreshToken,
		"expiry":        token.Expiry.Unix(),
	}
	enc, err := c.encryptMCPCredentials(creds)
	if err != nil {
		return "", err
	}
	connection.CredentialData = enc
	connection.CredentialsEncryptedV1 = true
	connection.Status = models.MCPConnectionStatusConnected
	connection.LastError = ""
	delete(connection.Config, "oauth_state")
	delete(connection.Config, "oauth_code_verifier")
	if _, err := c.DB.UpdateMCPConnection(connection); err != nil {
		return "", err
	}
	returnURL, _ := connection.Config["return_url"].(string)
	return returnURL, nil
}

func (c *Controller) DiscoverMCPConnectionTools(ctx context.Context, project *models.AppProject, connectionID string) (*models.MCPConnection, []models.MCPConnectionTool, error) {
	connection, err := c.DB.GetMCPConnectionByID(connectionID)
	if err != nil || connection.AppProjectID != project.ID {
		return nil, nil, fmt.Errorf("connection not found")
	}
	headers, err := c.mcpHeadersForConnection(ctx, connection)
	if err != nil {
		return nil, nil, err
	}
	discovered, err := mcpclient.DiscoverTools(ctx, mcpclient.ServerConfig{
		Name:           connection.Name,
		URL:            connection.ServerURL,
		Headers:        headers,
		Enabled:        true,
		TimeoutSeconds: 30,
	})
	if err != nil {
		connection.Status = models.MCPConnectionStatusError
		connection.LastError = err.Error()
		_, _ = c.DB.UpdateMCPConnection(connection)
		return nil, nil, err
	}
	now := time.Now()
	tools := make([]models.MCPConnectionTool, 0, len(discovered))
	for _, t := range discovered {
		display := t.Title
		if display == "" {
			display = t.Name
		}
		tools = append(tools, models.MCPConnectionTool{
			ConnectionID: connection.ID,
			Name:         t.Name,
			PrefixedName: mcpPrefixedToolName(connection.Slug, t.Name),
			DisplayName:  display,
			Description:  t.Description,
			InputSchema:  t.InputSchema,
			Annotations:  t.Annotations,
			RiskLevel:    classifyMCPRisk(t.Name, t.Annotations),
			Status:       "enabled",
			LastSeenAt:   now,
		})
	}
	if err := c.DB.ReplaceMCPConnectionTools(connection.ID, tools); err != nil {
		return nil, nil, err
	}
	connection.Status = models.MCPConnectionStatusConnected
	connection.LastError = ""
	connection.LastDiscoveredAt = now
	if _, err := c.DB.UpdateMCPConnection(connection); err != nil {
		return nil, nil, err
	}
	return connection, tools, nil
}

func (c *Controller) GetMCPConnections(project *models.AppProject) ([]responses.MCPConnectionResponse, error) {
	connections, err := c.DB.GetMCPConnectionsByProjectID(project.ID.String())
	if err != nil {
		return nil, err
	}
	resp := make([]responses.MCPConnectionResponse, 0, len(connections))
	for _, connection := range connections {
		tools, _ := c.DB.GetMCPConnectionTools(connection.ID.String())
		resp = append(resp, c.mcpConnectionResponse(connection, tools))
	}
	return resp, nil
}

func (c *Controller) GetSingleMCPConnectionResponse(connection *models.MCPConnection) responses.MCPConnectionResponse {
	if connection == nil {
		return responses.MCPConnectionResponse{}
	}
	tools, _ := c.DB.GetMCPConnectionTools(connection.ID.String())
	return c.mcpConnectionResponse(*connection, tools)
}

func (c *Controller) GetMCPConnectionResponse(connection models.MCPConnection, tools []models.MCPConnectionTool) responses.MCPConnectionResponse {
	return c.mcpConnectionResponse(connection, tools)
}

func (c *Controller) PutAgentMCPGrant(project *models.AppProject, agentID, connectionID string, req requests.PutAgentMCPGrantRequest) (*models.AgentMCPGrant, error) {
	connection, err := c.DB.GetMCPConnectionByID(connectionID)
	if err != nil || connection.AppProjectID != project.ID {
		return nil, fmt.Errorf("connection not found")
	}
	defaultPolicy := normalizeMCPPolicy(req.DefaultPolicy)
	if defaultPolicy == "" {
		defaultPolicy = models.MCPGrantPolicyAsk
	}
	toolPolicies := make(map[string]string)
	for key, val := range req.ToolPolicies {
		if key == "" {
			continue
		}
		policy := normalizeMCPPolicy(val)
		if policy == "" {
			continue
		}
		toolPolicies[key] = policy
	}
	grant := &models.AgentMCPGrant{
		AppProjectID:    project.ID,
		MCPConnectionID: connection.ID,
		AgentID:         agentID,
		Enabled:         req.Enabled,
		DefaultPolicy:   defaultPolicy,
		ToolPolicies:    toolPolicies,
	}
	return c.DB.UpsertAgentMCPGrant(grant)
}

func (c *Controller) GetAgentMCPGrantsForProject(project *models.AppProject) ([]responses.AgentMCPGrantResponse, error) {
	grants, err := c.DB.GetAgentMCPGrantsForProject(project.ID.String())
	if err != nil {
		return nil, err
	}
	resp := make([]responses.AgentMCPGrantResponse, 0, len(grants))
	for _, grant := range grants {
		resp = append(resp, agentMCPGrantResponse(grant))
	}
	return resp, nil
}

func (c *Controller) GetAgentMCPGrantsForOrg(orgID string) ([]responses.AgentMCPGrantResponse, error) {
	grants, err := c.DB.GetAgentMCPGrantsForOrg(orgID)
	if err != nil {
		return nil, err
	}
	resp := make([]responses.AgentMCPGrantResponse, 0, len(grants))
	for _, grant := range grants {
		resp = append(resp, agentMCPGrantResponse(grant))
	}
	return resp, nil
}

func (c *Controller) AgentMCPTools(projectID, agentID string) ([]agent.MCPRuntimeTool, error) {
	connections, err := c.DB.GetMCPConnectionsByProjectID(projectID)
	if err != nil {
		return nil, err
	}
	var result []agent.MCPRuntimeTool
	for _, connection := range connections {
		if connection.Status != models.MCPConnectionStatusConnected {
			continue
		}
		grant, err := c.DB.GetAgentMCPGrant(projectID, agentID, connection.ID.String())
		if err != nil || !grant.Enabled {
			continue
		}
		tools, err := c.DB.GetMCPConnectionTools(connection.ID.String())
		if err != nil {
			continue
		}
		for _, tool := range tools {
			policy := policyForMCPTool(grant, tool)
			if policy == models.MCPGrantPolicyBlocked {
				continue
			}
			result = append(result, agent.MCPRuntimeTool{
				ConnectionID: connection.ID.String(),
				ToolID:       tool.ID.String(),
				ToolName:     tool.Name,
				PrefixedName: tool.PrefixedName,
				DisplayName:  tool.DisplayName,
				Description:  tool.Description,
				InputSchema:  tool.InputSchema,
				RiskLevel:    tool.RiskLevel,
				Policy:       policy,
			})
		}
	}
	return result, nil
}

func (c *Controller) CreateMCPToolApproval(ctx context.Context, req agent.MCPToolApprovalRequest) (*agent.MCPToolApproval, error) {
	jobID, err := uuid.Parse(req.JobID)
	if err != nil {
		return nil, fmt.Errorf("invalid job id")
	}
	projectID, err := uuid.Parse(req.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project id")
	}
	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		return nil, fmt.Errorf("invalid user id")
	}
	connectionID, err := uuid.Parse(req.ConnectionID)
	if err != nil {
		return nil, fmt.Errorf("invalid connection id")
	}
	toolID, _ := uuid.Parse(req.ToolID)
	runID, _ := uuid.Parse(req.RunID)
	expiresAt := time.Now().Add(10 * time.Minute)
	approval := &models.MCPToolApproval{
		GenerationJobID:   jobID,
		AppProjectID:      projectID,
		ConnectionID:      connectionID,
		ToolID:            toolID,
		AgentID:           req.AgentID,
		ToolName:          req.ToolName,
		PrefixedName:      req.PrefixedName,
		RiskLevel:         nonEmpty(req.RiskLevel, agent.ToolRiskMedium),
		ArgumentsPreview:  json.RawMessage(truncateJSON(req.Arguments, 4096)),
		Status:            models.MCPApprovalStatusPending,
		RequestedByUserID: userID,
		ExpiresAt:         expiresAt,
	}
	if _, err := c.DB.CreateMCPToolApproval(approval); err != nil {
		return nil, err
	}
	_, _ = c.DB.CreateAgentMCPToolCall(&models.AgentMCPToolCall{
		AppProjectID:     projectID,
		ConnectionID:     connectionID,
		ToolID:           toolID,
		GenerationJobID:  jobID,
		RunID:            runID,
		ApprovalID:       approval.ID,
		AgentID:          req.AgentID,
		ToolName:         req.ToolName,
		PrefixedName:     req.PrefixedName,
		Policy:           models.MCPGrantPolicyAsk,
		RiskLevel:        approval.RiskLevel,
		ArgumentsPreview: approval.ArgumentsPreview,
		Status:           models.MCPToolCallStatusPending,
	})
	_ = c.DB.UpdateGenerationJobStatus(req.JobID, "awaiting_input", "")
	_ = ctx
	return &agent.MCPToolApproval{
		ID:              approval.ID.String(),
		Status:          approval.Status,
		ExpiresAtUnixMs: expiresAt.UnixMilli(),
	}, nil
}

func (c *Controller) WaitForMCPApproval(ctx context.Context, approvalID string) (agent.MCPApprovalDecision, error) {
	approval, err := c.DB.GetMCPToolApproval(approvalID)
	if err != nil {
		return agent.MCPApprovalDecision{}, err
	}
	if approval.Status != models.MCPApprovalStatusPending {
		return approvalDecisionFromModel(approval), nil
	}
	sub, err := jobstream.SubscribeApproval(ctx, c.RedisPubSub, approvalID)
	if err != nil {
		return agent.MCPApprovalDecision{}, err
	}
	defer sub.Close()
	approval, err = c.DB.GetMCPToolApproval(approvalID)
	if err == nil && approval.Status != models.MCPApprovalStatusPending {
		return approvalDecisionFromModel(approval), nil
	}

	timer := time.NewTimer(time.Until(approval.ExpiresAt))
	defer timer.Stop()
	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return agent.MCPApprovalDecision{}, ctx.Err()
		case <-timer.C:
			_ = c.DB.ExpireMCPToolApproval(approvalID)
			_ = c.DB.UpdateGenerationJobStatus(approval.GenerationJobID.String(), "running", "")
			if call, err := c.DB.GetAgentMCPToolCallByApprovalID(approvalID); err == nil {
				call.Status = models.MCPToolCallStatusDenied
				call.Error = "approval expired"
				_, _ = c.DB.UpdateAgentMCPToolCall(call)
			}
			return agent.MCPApprovalDecision{Approved: false, Status: models.MCPApprovalStatusExpired}, nil
		case msg, ok := <-ch:
			if !ok {
				return agent.MCPApprovalDecision{}, fmt.Errorf("approval subscription closed")
			}
			_ = msg
			approval, err := c.DB.GetMCPToolApproval(approvalID)
			if err != nil {
				return agent.MCPApprovalDecision{}, err
			}
			if approval.Status == models.MCPApprovalStatusPending {
				continue
			}
			_ = c.DB.UpdateGenerationJobStatus(approval.GenerationJobID.String(), "running", "")
			decision := approvalDecisionFromModel(approval)
			if !decision.Approved {
				if call, err := c.DB.GetAgentMCPToolCallByApprovalID(approvalID); err == nil {
					call.Status = models.MCPToolCallStatusDenied
					call.Error = "approval denied"
					_, _ = c.DB.UpdateAgentMCPToolCall(call)
				}
			}
			return decision, nil
		}
	}
}

func (c *Controller) ExecuteMCPTool(ctx context.Context, req agent.MCPToolExecutionRequest) (string, bool, error) {
	connection, err := c.DB.GetMCPConnectionByID(req.ConnectionID)
	if err != nil || connection.AppProjectID.String() != req.ProjectID || connection.Status != models.MCPConnectionStatusConnected {
		return "MCP connection is not available", true, nil
	}
	var call *models.AgentMCPToolCall
	if req.ApprovalID != "" {
		call, _ = c.DB.GetAgentMCPToolCallByApprovalID(req.ApprovalID)
	}
	if call == nil || call.ID == uuid.Nil {
		projectID, _ := uuid.Parse(req.ProjectID)
		connectionID, _ := uuid.Parse(req.ConnectionID)
		toolID, _ := uuid.Parse(req.ToolID)
		jobID, _ := uuid.Parse(req.JobID)
		runID, _ := uuid.Parse(req.RunID)
		approvalID, _ := uuid.Parse(req.ApprovalID)
		call = &models.AgentMCPToolCall{
			AppProjectID:     projectID,
			ConnectionID:     connectionID,
			ToolID:           toolID,
			GenerationJobID:  jobID,
			RunID:            runID,
			ApprovalID:       approvalID,
			AgentID:          req.AgentID,
			ToolName:         req.ToolName,
			PrefixedName:     req.PrefixedName,
			Policy:           normalizeMCPPolicy(req.Policy),
			RiskLevel:        nonEmpty(req.RiskLevel, agent.ToolRiskMedium),
			ArgumentsPreview: json.RawMessage(truncateJSON(req.Arguments, 4096)),
			Status:           models.MCPToolCallStatusPending,
		}
		call, _ = c.DB.CreateAgentMCPToolCall(call)
	}
	if req.ApprovedByUserID != "" {
		if uid, err := uuid.Parse(req.ApprovedByUserID); err == nil {
			call.ApprovedByUserID = &uid
		}
	}
	call.Status = models.MCPToolCallStatusApproved
	_, _ = c.DB.UpdateAgentMCPToolCall(call)

	headers, err := c.mcpHeadersForConnection(ctx, connection)
	if err != nil {
		call.Status = models.MCPToolCallStatusError
		call.Error = err.Error()
		_, _ = c.DB.UpdateAgentMCPToolCall(call)
		return "MCP credentials are unavailable", true, nil
	}
	start := time.Now()
	result, err := mcpclient.CallTool(ctx, mcpclient.ServerConfig{
		Name:           connection.Name,
		URL:            connection.ServerURL,
		Headers:        headers,
		Enabled:        true,
		TimeoutSeconds: 30,
	}, req.ToolName, string(req.Arguments))
	durationMs := int(time.Since(start).Milliseconds())
	call.DurationMs = durationMs
	call.ResultSummary = firstLineForAudit(result, 300)
	isErr := err != nil || strings.Contains(result, " returned an error")
	if isErr {
		call.Status = models.MCPToolCallStatusError
		if err != nil {
			call.Error = err.Error()
		} else {
			call.Error = call.ResultSummary
		}
	} else {
		call.Status = models.MCPToolCallStatusExecuted
	}
	_, _ = c.DB.UpdateAgentMCPToolCall(call)
	if err != nil {
		return result, true, err
	}
	return result, isErr, nil
}

func (c *Controller) SubmitMCPApproval(user *models.User, jobID, approvalID, decision string) (*models.MCPToolApproval, error) {
	approval, err := c.DB.GetMCPToolApproval(approvalID)
	if err != nil || approval.GenerationJobID.String() != jobID {
		return nil, fmt.Errorf("approval not found")
	}
	project, err := c.DB.GetAppProjectByID(approval.AppProjectID.String())
	if err != nil {
		return nil, fmt.Errorf("project not found")
	}
	if err := c.AuthorizeProjectAccess(user, project); err != nil {
		return nil, err
	}
	if _, err := c.RequirePermission(user, project.OrganizationID.String(), constants.PermCredentialsManage); err != nil {
		return nil, err
	}
	status := models.MCPApprovalStatusDenied
	if strings.EqualFold(decision, "approved") || strings.EqualFold(decision, "approve") {
		status = models.MCPApprovalStatusApproved
	}
	if err := c.DB.UpdateMCPToolApprovalDecision(approvalID, status, user.ID); err != nil {
		return nil, err
	}
	updated, err := c.DB.GetMCPToolApproval(approvalID)
	if err != nil {
		return nil, err
	}
	_ = jobstream.PublishApprovalDecision(context.Background(), c.RedisPubSub, approvalID, status)
	return updated, nil
}

func (c *Controller) encryptMCPCredentials(creds map[string]any) ([]byte, error) {
	enc := utils.EncryptIntegrationMapAny(creds, c.integrationCredentialsKey())
	return json.Marshal(enc)
}

func (c *Controller) decryptMCPCredentials(connection *models.MCPConnection) (map[string]any, error) {
	var creds map[string]any
	if connection == nil || len(connection.CredentialData) == 0 {
		return map[string]any{}, nil
	}
	if err := json.Unmarshal(connection.CredentialData, &creds); err != nil {
		return nil, err
	}
	return utils.DecryptIntegrationMapAny(creds, c.integrationCredentialsKey()), nil
}

func (c *Controller) mcpHeadersForConnection(ctx context.Context, connection *models.MCPConnection) (map[string]string, error) {
	creds, err := c.decryptMCPCredentials(connection)
	if err != nil {
		return nil, err
	}
	headers := map[string]string{}
	switch connection.AuthType {
	case models.MCPAuthTypeNone:
		return headers, nil
	case models.MCPAuthTypeOAuth:
		token, err := c.oauthTokenForConnection(ctx, connection, creds)
		if err != nil {
			return nil, err
		}
		if token == "" {
			return nil, fmt.Errorf("missing oauth access token")
		}
		headers["Authorization"] = "Bearer " + token
	case models.MCPAuthTypeAPIKey:
		if rawHeaders, ok := creds["headers"].(map[string]any); ok {
			for k, v := range rawHeaders {
				if s, ok := v.(string); ok && k != "" {
					headers[k] = s
				}
			}
			return headers, nil
		}
		apiKey, _ := creds["api_key"].(string)
		if apiKey == "" {
			apiKey, _ = creds["token"].(string)
		}
		if apiKey == "" {
			return nil, fmt.Errorf("missing api key")
		}
		headerName, _ := creds["header_name"].(string)
		if headerName == "" {
			headerName = "Authorization"
		}
		prefix, _ := creds["header_prefix"].(string)
		if prefix == "" && strings.EqualFold(headerName, "Authorization") {
			prefix = "Bearer"
		}
		if prefix != "" {
			headers[headerName] = prefix + " " + apiKey
		} else {
			headers[headerName] = apiKey
		}
	default:
		return nil, fmt.Errorf("unsupported auth type")
	}
	return headers, nil
}

func (c *Controller) oauthTokenForConnection(ctx context.Context, connection *models.MCPConnection, creds map[string]any) (string, error) {
	accessToken, _ := creds["access_token"].(string)
	refreshToken, _ := creds["refresh_token"].(string)
	expiryUnix := int64FromAny(creds["expiry"])
	if accessToken == "" || refreshToken == "" || expiryUnix == 0 || time.Unix(expiryUnix, 0).After(time.Now().Add(90*time.Second)) {
		return accessToken, nil
	}
	cfg, err := c.oauthConfigForConnection(connection)
	if err != nil {
		return accessToken, nil
	}
	tok := &oauth2.Token{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		Expiry:       time.Unix(expiryUnix, 0),
	}
	fresh, err := cfg.TokenSource(ctx, tok).Token()
	if err != nil {
		return accessToken, nil
	}
	updated := map[string]any{
		"access_token":  fresh.AccessToken,
		"token_type":    fresh.TokenType,
		"refresh_token": fresh.RefreshToken,
		"expiry":        fresh.Expiry.Unix(),
	}
	enc, err := c.encryptMCPCredentials(updated)
	if err == nil {
		connection.CredentialData = enc
		_, _ = c.DB.UpdateMCPConnection(connection)
	}
	return fresh.AccessToken, nil
}

func (c *Controller) oauthConfigForConnection(connection *models.MCPConnection) (*oauth2.Config, error) {
	if connection.Config == nil {
		return nil, fmt.Errorf("oauth config is missing")
	}
	authURL, _ := connection.Config["authorization_endpoint"].(string)
	tokenURL, _ := connection.Config["token_endpoint"].(string)
	clientID, _ := connection.Config["client_id"].(string)
	creds, _ := c.decryptMCPCredentials(connection)
	clientSecret, _ := creds["client_secret"].(string)
	if authURL == "" || tokenURL == "" || clientID == "" {
		return nil, fmt.Errorf("oauth authorization_endpoint, token_endpoint, and client_id are required")
	}
	scopes := stringSliceFromAny(connection.Config["scopes"])
	if len(scopes) == 0 {
		if template := resolveMCPTemplate(connection.TemplateID); template != nil {
			scopes = template.Scopes
		}
	}
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  authURL,
			TokenURL: tokenURL,
		},
		RedirectURL: c.mcpOAuthRedirectURL(),
		Scopes:      scopes,
	}, nil
}

func (c *Controller) mcpOAuthRedirectURL() string {
	if v := strings.TrimSpace(c.SecretManager.GetSecret("MCP_OAUTH_REDIRECT_URL")); v != "" {
		return v
	}
	return strings.TrimRight(constants.WEMOB_API_BASE_URL, "/") + "/v2/mcp/oauth/callback"
}

func (c *Controller) findMCPConnectionByOAuthState(state string) (*models.MCPConnection, error) {
	var connections []models.MCPConnection
	err := c.DB.DB.NewSelect().
		Model(&connections).
		Where("deleted_at IS NULL").
		Where("config->>'oauth_state' = ?", state).
		Limit(1).
		Scan(context.Background())
	if err != nil || len(connections) == 0 {
		return nil, fmt.Errorf("oauth state not found")
	}
	return &connections[0], nil
}

func (c *Controller) mcpConnectionResponse(connection models.MCPConnection, tools []models.MCPConnectionTool) responses.MCPConnectionResponse {
	toolResp := make([]responses.MCPToolResponse, 0, len(tools))
	for _, tool := range tools {
		toolResp = append(toolResp, responses.MCPToolResponse{
			ID:           tool.ID,
			ConnectionID: tool.ConnectionID,
			Name:         tool.Name,
			PrefixedName: tool.PrefixedName,
			DisplayName:  tool.DisplayName,
			Description:  tool.Description,
			InputSchema:  tool.InputSchema,
			Annotations:  tool.Annotations,
			RiskLevel:    tool.RiskLevel,
			Status:       tool.Status,
		})
	}
	var discoveredAt *time.Time
	if !connection.LastDiscoveredAt.IsZero() {
		t := connection.LastDiscoveredAt
		discoveredAt = &t
	}
	return responses.MCPConnectionResponse{
		ID:               connection.ID,
		AppProjectID:     connection.AppProjectID,
		Name:             connection.Name,
		Slug:             connection.Slug,
		TemplateID:       connection.TemplateID,
		AuthType:         connection.AuthType,
		ServerURL:        connection.ServerURL,
		Status:           connection.Status,
		Config:           publicMCPConfig(connection.Config),
		LastDiscoveredAt: discoveredAt,
		LastError:        connection.LastError,
		Tools:            toolResp,
		CreatedAt:        connection.CreatedAt,
		UpdatedAt:        connection.UpdatedAt,
	}
}

func agentMCPGrantResponse(grant models.AgentMCPGrant) responses.AgentMCPGrantResponse {
	return responses.AgentMCPGrantResponse{
		ID:            grant.ID,
		AppProjectID:  grant.AppProjectID,
		ConnectionID:  grant.MCPConnectionID,
		AgentID:       grant.AgentID,
		Enabled:       grant.Enabled,
		DefaultPolicy: grant.DefaultPolicy,
		ToolPolicies:  grant.ToolPolicies,
		CreatedAt:     grant.CreatedAt,
		UpdatedAt:     grant.UpdatedAt,
	}
}

func resolveMCPTemplate(id string) *mcpTemplate {
	for _, t := range mcpTemplates {
		if t.ID == id {
			copy := t
			return &copy
		}
	}
	return nil
}

func normalizeMCPAuthType(authType string) string {
	switch strings.ToLower(strings.TrimSpace(authType)) {
	case "", "none", "api_key", "oauth":
		return strings.ToLower(strings.TrimSpace(authType))
	default:
		return ""
	}
}

func normalizeMCPPolicy(policy string) string {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case models.MCPGrantPolicyAuto, models.MCPGrantPolicyAsk, models.MCPGrantPolicyBlocked:
		return strings.ToLower(strings.TrimSpace(policy))
	default:
		return ""
	}
}

func policyForMCPTool(grant *models.AgentMCPGrant, tool models.MCPConnectionTool) string {
	if grant == nil {
		return models.MCPGrantPolicyAsk
	}
	for _, key := range []string{tool.PrefixedName, tool.Name, tool.ID.String()} {
		if policy := normalizeMCPPolicy(grant.ToolPolicies[key]); policy != "" {
			return policy
		}
	}
	if policy := normalizeMCPPolicy(grant.DefaultPolicy); policy != "" {
		return policy
	}
	if tool.RiskLevel == agent.ToolRiskLow {
		return models.MCPGrantPolicyAuto
	}
	return models.MCPGrantPolicyAsk
}

func classifyMCPRisk(toolName string, annotations map[string]any) string {
	if boolFromAny(annotations["readOnlyHint"]) {
		return agent.ToolRiskLow
	}
	if boolPtrFalse(annotations["destructiveHint"]) {
		return agent.ToolRiskHigh
	}
	if boolFromAny(annotations["destructiveHint"]) {
		return agent.ToolRiskDestructive
	}
	name := strings.ToLower(toolName)
	switch {
	case strings.HasPrefix(name, "get"), strings.HasPrefix(name, "list"), strings.HasPrefix(name, "read"), strings.Contains(name, "search"), strings.Contains(name, "lookup"):
		return agent.ToolRiskLow
	case strings.Contains(name, "delete"), strings.Contains(name, "remove"), strings.Contains(name, "destroy"):
		return agent.ToolRiskDestructive
	case strings.Contains(name, "send"), strings.Contains(name, "create"), strings.Contains(name, "update"), strings.Contains(name, "write"), strings.Contains(name, "charge"), strings.Contains(name, "transfer"):
		return agent.ToolRiskHigh
	default:
		return agent.ToolRiskMedium
	}
}

func mcpPrefixedToolName(connectionSlug, toolName string) string {
	return "mcp_" + safeToolSegment(connectionSlug) + "__" + safeToolSegment(toolName)
}

func safeToolSegment(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "tool"
	}
	return out
}

func publicMCPConfig(config map[string]any) map[string]any {
	out := make(map[string]any)
	for k, v := range config {
		switch k {
		case "client_secret", "oauth_state", "oauth_code_verifier":
			continue
		default:
			out[k] = v
		}
	}
	return out
}

func sanitizeMCPConfig(config map[string]any) map[string]any {
	if config == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(config))
	for k, v := range config {
		if k == "client_secret" {
			continue
		}
		out[k] = v
	}
	return out
}

func validateHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("server_url must be a valid URL")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("server_url must use http or https")
	}
	return nil
}

func randomOAuthString(bytesLen int) string {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return uuid.NewString()
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func oauthS256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func stringSliceFromAny(v any) []string {
	switch typed := v.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if typed == "" {
			return nil
		}
		return strings.Fields(typed)
	default:
		return nil
	}
}

func boolFromAny(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

func boolPtrFalse(v any) bool {
	b, ok := v.(bool)
	return ok && !b
}

func int64FromAny(v any) int64 {
	switch typed := v.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	case json.Number:
		n, _ := typed.Int64()
		return n
	default:
		return 0
	}
}

func truncateJSON(raw json.RawMessage, limit int) string {
	if len(raw) == 0 {
		return "{}"
	}
	if len(raw) <= limit {
		return string(raw)
	}
	truncated, _ := json.Marshal(map[string]any{
		"truncated": true,
		"preview":   string(raw[:limit]),
	})
	return string(truncated)
}

func approvalDecisionFromModel(approval *models.MCPToolApproval) agent.MCPApprovalDecision {
	if approval == nil {
		return agent.MCPApprovalDecision{Approved: false, Status: models.MCPApprovalStatusDenied}
	}
	userID := ""
	if approval.DecidedByUserID != nil {
		userID = approval.DecidedByUserID.String()
	}
	return agent.MCPApprovalDecision{
		Approved: approval.Status == models.MCPApprovalStatusApproved,
		Status:   approval.Status,
		UserID:   userID,
	}
}

func firstLineForAudit(s string, max int) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx]
	}
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
