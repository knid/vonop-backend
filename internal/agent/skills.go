package agent

import (
	"embed"
	"fmt"
	"io/fs"
	"slices"
	"sort"
	"strings"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
	"gopkg.in/yaml.v3"
)

//go:embed skills/*/*/SKILL.md
var embeddedSkillFS embed.FS

type SkillFile struct {
	ID           string
	Name         string
	Description  string
	Origin       string
	Agents       []string
	ProjectTypes []string
	Triggers     []string
	AllowedTools []string
	RiskLevel    string
	Default      bool
	Path         string
	Body         string
}

type skillFrontmatter struct {
	ID           string   `yaml:"id"`
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	Origin       string   `yaml:"origin"`
	Agents       []string `yaml:"agents"`
	ProjectTypes []string `yaml:"project_types"`
	Triggers     []string `yaml:"triggers"`
	AllowedTools []string `yaml:"allowed_tools"`
	RiskLevel    string   `yaml:"risk_level"`
	Default      bool     `yaml:"default"`
}

type SkillCatalog struct {
	Skills []SkillFile
	ByID   map[string]SkillFile
}

type SkillManifestEntry struct {
	ID           string   `json:"id"`
	Name         string   `json:"name,omitempty"`
	Description  string   `json:"description,omitempty"`
	Origin       string   `json:"origin,omitempty"`
	Agents       []string `json:"agents,omitempty"`
	ProjectTypes []string `json:"project_types,omitempty"`
	Triggers     []string `json:"triggers,omitempty"`
	RiskLevel    string   `json:"risk_level,omitempty"`
	Required     bool     `json:"required,omitempty"`
	Loaded       bool     `json:"loaded,omitempty"`
	Path         string   `json:"path,omitempty"`
}

func LoadEmbeddedSkillCatalog() ([]SkillFile, error) {
	var out []SkillFile
	err := fs.WalkDir(embeddedSkillFS, "skills", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, "/SKILL.md") {
			return nil
		}
		raw, err := embeddedSkillFS.ReadFile(path)
		if err != nil {
			return err
		}
		skill, err := parseSkillFile(path, string(raw))
		if err != nil {
			return err
		}
		out = append(out, skill)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func ParseSkillMarkdown(path, raw string) (SkillFile, error) {
	return parseSkillFile(path, raw)
}

func NewSkillCatalog(skills []SkillFile) SkillCatalog {
	byID := make(map[string]SkillFile, len(skills))
	out := make([]SkillFile, 0, len(skills))
	for _, skill := range skills {
		if skill.ID == "" {
			continue
		}
		if _, exists := byID[skill.ID]; exists {
			continue
		}
		byID[skill.ID] = skill
		out = append(out, skill)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return SkillCatalog{Skills: out, ByID: byID}
}

func ResolveSkills(profile *AgentProfile, project *models.AppProject, message string, isFirstGen bool) []SkillFile {
	catalog, err := LoadEmbeddedSkillCatalog()
	if err != nil {
		return nil
	}
	return resolveRequiredSkillsFromCatalog(NewSkillCatalog(catalog), profile, project)
}

func ResolveSuggestedSkills(profile *AgentProfile, project *models.AppProject, message string, isFirstGen bool) []SkillFile {
	catalog, err := LoadEmbeddedSkillCatalog()
	if err != nil {
		return nil
	}
	return resolveSuggestedSkillsFromCatalog(NewSkillCatalog(catalog), profile, project, message, isFirstGen)
}

func ResolveCompatibleSkills(profile *AgentProfile, project *models.AppProject) []SkillFile {
	catalog, err := LoadEmbeddedSkillCatalog()
	if err != nil {
		return nil
	}
	return compatibleSkillsFromCatalog(NewSkillCatalog(catalog), profile, project)
}

func RenderSkillMessages(skills []SkillFile) []utils.Message {
	if len(skills) == 0 {
		return nil
	}
	out := make([]utils.Message, 0, len(skills))
	for _, skill := range skills {
		out = append(out, utils.Message{
			Role:    "system",
			Content: skill.Render(),
		})
	}
	return out
}

func RenderSkillManifestMessage(skills, requiredSkills, suggestedSkills []SkillFile) utils.Message {
	required := skillIDSet(requiredSkills)
	suggestedIDs := skillIDs(suggestedSkills)
	var sb strings.Builder
	sb.WriteString("# AVAILABLE SKILLS\n")
	sb.WriteString("Required skills are already loaded as system messages. Optional skills are listed as a compact manifest; call `load_skill` before relying on an optional skill's detailed instructions. Use `search_skills` when the relevant skill is unclear.\n\n")
	if len(required) > 0 {
		sb.WriteString("Required loaded skills:\n")
		for _, id := range skillIDs(requiredSkills) {
			sb.WriteString("- ")
			sb.WriteString(id)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}
	if len(suggestedIDs) > 0 {
		sb.WriteString("Suggested for this request:\n")
		for _, id := range suggestedIDs {
			if !required[id] {
				sb.WriteString("- ")
				sb.WriteString(id)
				sb.WriteString(" (call load_skill if needed)\n")
			}
		}
		sb.WriteString("\n")
	}
	sb.WriteString("Skill manifest:\n")
	for _, skill := range skills {
		entry := skillManifestEntry(skill, required[skill.ID], false)
		sb.WriteString("- id: ")
		sb.WriteString(entry.ID)
		if entry.Required {
			sb.WriteString(" | required: true")
		}
		if entry.Name != "" {
			sb.WriteString(" | name: ")
			sb.WriteString(entry.Name)
		}
		if entry.RiskLevel != "" {
			sb.WriteString(" | risk: ")
			sb.WriteString(entry.RiskLevel)
		}
		if len(entry.Agents) > 0 {
			sb.WriteString(" | agents: ")
			sb.WriteString(strings.Join(entry.Agents, ","))
		}
		if len(entry.ProjectTypes) > 0 {
			sb.WriteString(" | project_types: ")
			sb.WriteString(strings.Join(entry.ProjectTypes, ","))
		}
		if len(entry.Triggers) > 0 {
			sb.WriteString(" | triggers: ")
			sb.WriteString(strings.Join(entry.Triggers, ","))
		}
		if entry.Description != "" {
			sb.WriteString(" | description: ")
			sb.WriteString(entry.Description)
		}
		sb.WriteString("\n")
	}
	return utils.Message{Role: "system", Content: sb.String()}
}

func skillIDs(skills []SkillFile) []string {
	out := make([]string, 0, len(skills))
	for _, skill := range skills {
		out = append(out, skill.ID)
	}
	sort.Strings(out)
	return out
}

func sortSkills(skills []SkillFile) {
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].ID < skills[j].ID
	})
}

func skillIDSet(skills []SkillFile) map[string]bool {
	out := make(map[string]bool, len(skills))
	for _, skill := range skills {
		if skill.ID != "" {
			out[skill.ID] = true
		}
	}
	return out
}

func (s SkillFile) Render() string {
	name := strings.TrimSpace(s.Name)
	if name == "" {
		name = s.ID
	}
	var sb strings.Builder
	sb.WriteString("# SKILL: ")
	sb.WriteString(name)
	sb.WriteString("\n")
	if s.Description != "" {
		sb.WriteString("Description: ")
		sb.WriteString(s.Description)
		sb.WriteString("\n")
	}
	if s.Origin != "" {
		sb.WriteString("Origin: ")
		sb.WriteString(s.Origin)
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	sb.WriteString(strings.TrimSpace(s.Body))
	return sb.String()
}

func resolveSkillsFromCatalog(catalog SkillCatalog, profile *AgentProfile, project *models.AppProject, message string, isFirstGen bool) []SkillFile {
	selected := map[string]SkillFile{}
	for _, skill := range resolveRequiredSkillsFromCatalog(catalog, profile, project) {
		selected[skill.ID] = skill
	}
	for _, skill := range resolveSuggestedSkillsFromCatalog(catalog, profile, project, message, isFirstGen) {
		selected[skill.ID] = skill
	}
	out := make([]SkillFile, 0, len(selected))
	for _, skill := range selected {
		out = append(out, skill)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func resolveRequiredSkillsFromCatalog(catalog SkillCatalog, profile *AgentProfile, project *models.AppProject) []SkillFile {
	if profile == nil {
		profile = ResolveAgentProfile(project)
	}
	projectType := models.ProjectTypeApp
	if project != nil && project.ProjectType != "" {
		projectType = project.ProjectType
	}

	selected := map[string]SkillFile{}
	add := func(skill SkillFile) {
		if project.UsesSplitWebRuntime() && skill.ID == "nextjs-app-router" {
			return
		}
		if !skillCompatible(skill, profile, projectType) {
			return
		}
		selected[skill.ID] = skill
	}

	for _, skill := range catalog.Skills {
		if skill.Default || slices.Contains(profile.DefaultSkillIDs, skill.ID) {
			add(skill)
		}
	}
	out := make([]SkillFile, 0, len(selected))
	for _, skill := range selected {
		out = append(out, skill)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func resolveSuggestedSkillsFromCatalog(catalog SkillCatalog, profile *AgentProfile, project *models.AppProject, message string, isFirstGen bool) []SkillFile {
	if profile == nil {
		profile = ResolveAgentProfile(project)
	}
	projectType := models.ProjectTypeApp
	if project != nil && project.ProjectType != "" {
		projectType = project.ProjectType
	}
	messageLower := strings.ToLower(message)
	explicitIDs := SlashCommandSkillIDs(message)

	selected := map[string]SkillFile{}
	required := skillIDSet(resolveRequiredSkillsFromCatalog(catalog, profile, project))
	add := func(skill SkillFile) {
		if required[skill.ID] || !skillCompatible(skill, profile, projectType) {
			return
		}
		selected[skill.ID] = skill
	}
	for _, id := range explicitIDs {
		if skill, ok := catalog.ByID[id]; ok {
			add(skill)
		}
	}
	for _, skill := range catalog.Skills {
		for _, trigger := range skill.Triggers {
			trigger = strings.TrimSpace(strings.ToLower(trigger))
			if trigger == "" {
				continue
			}
			if strings.Contains(messageLower, trigger) {
				add(skill)
				break
			}
		}
	}
	if isFirstGen {
		for _, skill := range catalog.Skills {
			if slices.Contains(skill.Triggers, "first_generation") {
				add(skill)
			}
		}
	}

	out := make([]SkillFile, 0, len(selected))
	for _, skill := range selected {
		out = append(out, skill)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func compatibleSkillsFromCatalog(catalog SkillCatalog, profile *AgentProfile, project *models.AppProject) []SkillFile {
	if profile == nil {
		profile = ResolveAgentProfile(project)
	}
	projectType := models.ProjectTypeApp
	if project != nil && project.ProjectType != "" {
		projectType = project.ProjectType
	}
	out := make([]SkillFile, 0, len(catalog.Skills))
	for _, skill := range catalog.Skills {
		if skillCompatible(skill, profile, projectType) {
			out = append(out, skill)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func skillManifestEntry(skill SkillFile, required, loaded bool) SkillManifestEntry {
	return SkillManifestEntry{
		ID:           skill.ID,
		Name:         skill.Name,
		Description:  skill.Description,
		Origin:       skill.Origin,
		Agents:       append([]string(nil), skill.Agents...),
		ProjectTypes: append([]string(nil), skill.ProjectTypes...),
		Triggers:     append([]string(nil), skill.Triggers...),
		RiskLevel:    skill.RiskLevel,
		Required:     required,
		Loaded:       loaded,
		Path:         skill.Path,
	}
}

func skillCompatible(skill SkillFile, profile *AgentProfile, projectType string) bool {
	if profile == nil {
		return false
	}
	if len(skill.Agents) > 0 && !slices.Contains(skill.Agents, profile.ID) {
		return false
	}
	if len(skill.ProjectTypes) > 0 && !slices.Contains(skill.ProjectTypes, projectType) {
		return false
	}
	return true
}

func parseSkillFile(path, raw string) (SkillFile, error) {
	raw = strings.TrimPrefix(raw, "\ufeff")
	if !strings.HasPrefix(raw, "---\n") {
		return SkillFile{}, fmt.Errorf("skill %s missing YAML frontmatter", path)
	}
	rest := raw[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return SkillFile{}, fmt.Errorf("skill %s has unterminated YAML frontmatter", path)
	}
	fmRaw := rest[:end]
	body := strings.TrimSpace(rest[end+len("\n---"):])
	var fm skillFrontmatter
	if err := yaml.Unmarshal([]byte(fmRaw), &fm); err != nil {
		return SkillFile{}, fmt.Errorf("skill %s frontmatter: %w", path, err)
	}
	if strings.TrimSpace(fm.ID) == "" {
		return SkillFile{}, fmt.Errorf("skill %s missing id", path)
	}
	name := strings.TrimSpace(fm.Name)
	if name == "" {
		name = fm.ID
	}
	return SkillFile{
		ID:           strings.TrimSpace(fm.ID),
		Name:         name,
		Description:  strings.TrimSpace(fm.Description),
		Origin:       strings.TrimSpace(fm.Origin),
		Agents:       cleanSkillStringList(fm.Agents),
		ProjectTypes: cleanSkillStringList(fm.ProjectTypes),
		Triggers:     cleanSkillStringList(fm.Triggers),
		AllowedTools: cleanSkillStringList(fm.AllowedTools),
		RiskLevel:    strings.TrimSpace(fm.RiskLevel),
		Default:      fm.Default,
		Path:         path,
		Body:         body,
	}, nil
}

func cleanSkillStringList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
