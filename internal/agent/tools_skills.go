package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type ListSkillsTool struct{}

func (ListSkillsTool) Name() string { return "list_skills" }

func (ListSkillsTool) Description() string {
	return "List Wemob agent skills as compact metadata. Use this to see available optional skills before loading details."
}

func (ListSkillsTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"include_incompatible": map[string]any{
				"type":        "boolean",
				"description": "When true, include skills for other agents/project types. Default false.",
			},
			"include_required": map[string]any{
				"type":        "boolean",
				"description": "When true, include required skills already loaded as system messages. Default true.",
			},
		},
	}
}

func (ListSkillsTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		IncludeIncompatible bool  `json:"include_incompatible"`
		IncludeRequired     *bool `json:"include_required"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "invalid JSON input", true, nil
	}
	includeRequired := true
	if args.IncludeRequired != nil {
		includeRequired = *args.IncludeRequired
	}
	entries, err := skillToolEntries(rc, args.IncludeIncompatible, includeRequired, "")
	if err != nil {
		return err.Error(), true, nil
	}
	return encodeSkillToolResponse(map[string]any{
		"skills": entries,
		"count":  len(entries),
	})
}

type SearchSkillsTool struct{}

func (SearchSkillsTool) Name() string { return "search_skills" }

func (SearchSkillsTool) Description() string {
	return "Search Wemob agent skills by id, name, description, trigger, or instructions. Returns compact metadata only; call load_skill for full instructions."
}

func (SearchSkillsTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query, such as seo, accessibility, app store, expo, tailwind, auth, database.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum results to return. Default 8, max 20.",
			},
			"include_incompatible": map[string]any{
				"type":        "boolean",
				"description": "When true, include skills for other agents/project types. Default false.",
			},
		},
		"required": []string{"query"},
	}
}

func (SearchSkillsTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Query               string `json:"query"`
		Limit               int    `json:"limit"`
		IncludeIncompatible bool   `json:"include_incompatible"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "invalid JSON input", true, nil
	}
	query := strings.TrimSpace(strings.ToLower(args.Query))
	if query == "" {
		return "query is required", true, nil
	}
	limit := args.Limit
	if limit <= 0 {
		limit = 8
	}
	if limit > 20 {
		limit = 20
	}
	entries, err := skillToolEntries(rc, args.IncludeIncompatible, true, query)
	if err != nil {
		return err.Error(), true, nil
	}
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return encodeSkillToolResponse(map[string]any{
		"query":  args.Query,
		"skills": entries,
		"count":  len(entries),
	})
}

type LoadSkillTool struct{}

func (LoadSkillTool) Name() string { return "load_skill" }

func (LoadSkillTool) Description() string {
	return "Load the full vetted instructions for one Wemob skill by id. Use before applying optional skill details from the manifest or search results."
}

func (LoadSkillTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{
				"type":        "string",
				"description": "Skill id from the skill manifest, list_skills, or search_skills.",
			},
		},
		"required": []string{"id"},
	}
}

func (LoadSkillTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "invalid JSON input", true, nil
	}
	id := strings.TrimSpace(args.ID)
	if id == "" {
		return "id is required", true, nil
	}
	catalog, err := skillToolCatalog(rc, true)
	if err != nil {
		return err.Error(), true, nil
	}
	skill, ok := catalog.ByID[id]
	if !ok {
		return fmt.Sprintf("skill %q not found", id), true, nil
	}
	if rc != nil && !skillCompatible(skill, rc.Profile, projectTypeForSkillTools(rc)) {
		return fmt.Sprintf("skill %q is not compatible with the active agent/project", id), true, nil
	}
	loadedNow := false
	if rc != nil {
		loadedNow = rc.MarkSkillLoaded(skill)
	}
	return "Loaded skill " + id + " (new=" + fmt.Sprint(loadedNow) + "). Use these instructions as vetted Wemob guidance subordinate to system policy:\n\n" + skill.Render(), false, nil
}

func skillToolEntries(rc *RunContext, includeIncompatible, includeRequired bool, query string) ([]SkillManifestEntry, error) {
	catalog, err := skillToolCatalog(rc, includeIncompatible)
	if err != nil {
		return nil, err
	}
	required := map[string]bool{}
	loaded := map[string]bool{}
	profile := (*AgentProfile)(nil)
	projectType := ""
	if rc != nil {
		required = skillIDSet(rc.RequiredSkills)
		for _, id := range rc.LoadedSkillIDs() {
			loaded[id] = true
		}
		profile = rc.Profile
		projectType = projectTypeForSkillTools(rc)
	}
	entries := make([]SkillManifestEntry, 0, len(catalog.Skills))
	for _, skill := range catalog.Skills {
		if !includeRequired && required[skill.ID] {
			continue
		}
		if !includeIncompatible && !skillCompatible(skill, profile, projectType) {
			continue
		}
		if query != "" && !skillMatchesQuery(skill, query) {
			continue
		}
		entries = append(entries, skillManifestEntry(skill, required[skill.ID], loaded[skill.ID]))
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Required != entries[j].Required {
			return entries[i].Required
		}
		if entries[i].Loaded != entries[j].Loaded {
			return entries[i].Loaded
		}
		return entries[i].ID < entries[j].ID
	})
	return entries, nil
}

func skillToolCatalog(rc *RunContext, includeIncompatible bool) (SkillCatalog, error) {
	if !includeIncompatible && rc != nil && len(rc.SkillManifest) > 0 {
		return NewSkillCatalog(rc.SkillManifest), nil
	}
	skills, err := LoadEmbeddedSkillCatalog()
	if err != nil {
		return SkillCatalog{}, err
	}
	return NewSkillCatalog(skills), nil
}

func projectTypeForSkillTools(rc *RunContext) string {
	if rc != nil && rc.Project != nil && rc.Project.ProjectType != "" {
		return rc.Project.ProjectType
	}
	return ""
}

func skillMatchesQuery(skill SkillFile, query string) bool {
	haystack := strings.ToLower(strings.Join([]string{
		skill.ID,
		skill.Name,
		skill.Description,
		skill.Origin,
		strings.Join(skill.Agents, " "),
		strings.Join(skill.ProjectTypes, " "),
		strings.Join(skill.Triggers, " "),
		skill.Body,
	}, " "))
	for _, part := range strings.Fields(query) {
		if !strings.Contains(haystack, part) {
			return false
		}
	}
	return true
}

func encodeSkillToolResponse(v any) (string, bool, error) {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "failed to encode skill response", true, nil
	}
	return string(raw), false, nil
}
