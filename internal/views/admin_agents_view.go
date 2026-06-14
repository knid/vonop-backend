package views

import (
	"net/http"

	"github.com/knid/wemob/internal/agent"
	"github.com/knid/wemob/internal/utils"
)

type adminAgentProfileResponse struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	ProjectTypes    []string `json:"project_types"`
	DefaultSkillIDs []string `json:"default_skill_ids"`
	HandoffTargets  []string `json:"handoff_targets"`
}

type adminAgentSkillResponse struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	Origin       string                 `json:"origin"`
	Agents       []string               `json:"agents"`
	ProjectTypes []string               `json:"project_types"`
	Triggers     []string               `json:"triggers"`
	AllowedTools []string               `json:"allowed_tools"`
	RiskLevel    string                 `json:"risk_level"`
	Default      bool                   `json:"default"`
	Path         string                 `json:"path"`
	Lint         []agent.SkillLintIssue `json:"lint"`
}

type adminAgentToolRiskResponse struct {
	Name string `json:"name"`
	Risk string `json:"risk"`
}

type adminAgentSystemResponse struct {
	Profiles []adminAgentProfileResponse  `json:"profiles"`
	Skills   []adminAgentSkillResponse    `json:"skills"`
	Tools    []adminAgentToolRiskResponse `json:"tools"`
	Settings []string                     `json:"settings"`
}

type adminLintSkillRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type adminLintSkillResponse struct {
	Skill  *adminAgentSkillResponse `json:"skill,omitempty"`
	Issues []agent.SkillLintIssue   `json:"issues"`
	Error  string                   `json:"error,omitempty"`
}

// GetAdminAgentSystem handles GET /v2/admin/agents.
func (v *HttpViews) GetAdminAgentSystem(w http.ResponseWriter, r *http.Request) {
	catalog, err := agent.LoadEmbeddedSkillCatalog()
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	profiles := agent.ListAgentProfiles()
	profileResponses := make([]adminAgentProfileResponse, len(profiles))
	for i, profile := range profiles {
		profileResponses[i] = adminAgentProfileResponse{
			ID:              profile.ID,
			Name:            profile.Name,
			ProjectTypes:    append([]string(nil), profile.ProjectTypes...),
			DefaultSkillIDs: append([]string(nil), profile.DefaultSkillIDs...),
			HandoffTargets:  append([]string(nil), profile.HandoffTargets...),
		}
	}

	skillResponses := make([]adminAgentSkillResponse, len(catalog))
	for i, skill := range catalog {
		skillResponses[i] = toAdminAgentSkillResponse(skill)
	}

	utils.JSONResponse(w, adminAgentSystemResponse{
		Profiles: profileResponses,
		Skills:   skillResponses,
		Tools:    adminAgentToolRisks(),
		Settings: []string{
			"agent_require_approval",
			"agent_enforce_skill_tool_allowlist",
		},
	}, http.StatusOK)
}

// LintAdminAgentSkill handles POST /v2/admin/agents/skills/lint.
func (v *HttpViews) LintAdminAgentSkill(w http.ResponseWriter, r *http.Request) {
	var req adminLintSkillRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}
	if req.Path == "" {
		req.Path = "imported/SKILL.md"
	}
	skill, err := agent.ParseSkillMarkdown(req.Path, req.Content)
	if err != nil {
		utils.JSONResponse(w, adminLintSkillResponse{
			Issues: []agent.SkillLintIssue{{
				Severity: agent.SkillLintError,
				Field:    "frontmatter",
				Message:  err.Error(),
			}},
			Error: err.Error(),
		}, http.StatusOK)
		return
	}
	resp := toAdminAgentSkillResponse(skill)
	utils.JSONResponse(w, adminLintSkillResponse{
		Skill:  &resp,
		Issues: resp.Lint,
	}, http.StatusOK)
}

func toAdminAgentSkillResponse(skill agent.SkillFile) adminAgentSkillResponse {
	return adminAgentSkillResponse{
		ID:           skill.ID,
		Name:         skill.Name,
		Description:  skill.Description,
		Origin:       skill.Origin,
		Agents:       append([]string(nil), skill.Agents...),
		ProjectTypes: append([]string(nil), skill.ProjectTypes...),
		Triggers:     append([]string(nil), skill.Triggers...),
		AllowedTools: append([]string(nil), skill.AllowedTools...),
		RiskLevel:    skill.RiskLevel,
		Default:      skill.Default,
		Path:         skill.Path,
		Lint:         agent.LintSkillFile(skill),
	}
}

func adminAgentToolRisks() []adminAgentToolRiskResponse {
	names := []string{
		"read_files",
		"list_files",
		"grep",
		"read_attachment",
		"read_mobile_config",
		"read_build_logs",
		"read_deploy_logs",
		"web_search",
		"write_file",
		"write_files",
		"edit_files",
		"delete_file",
		"generate_image",
		"execute_sql",
		"provision_database",
		"provision_local_database",
		"setup_wemob_ai_gateway",
		"setup_store",
		"configure_mobile_bridge",
		"trigger_build_after_fix",
		"trigger_web_deploy",
		"sandbox_status",
		"sandbox_exec",
		"sandbox_read_file",
		"sandbox_write_file",
		"sandbox_list_files",
		"sandbox_read_logs",
		"sandbox_serve_artifact",
		"preview_set_url",
		"preview_open_route",
		"preview_open_artifact",
	}
	out := make([]adminAgentToolRiskResponse, len(names))
	for i, name := range names {
		out[i] = adminAgentToolRiskResponse{Name: name, Risk: agent.ClassifyToolRisk(name)}
	}
	return out
}
