package agent

import (
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

type AgentContextPacket struct {
	Profile           *AgentProfile
	SystemMessages    []utils.Message
	Skills            []SkillFile
	RequiredSkills    []SkillFile
	SuggestedSkills   []SkillFile
	SkillManifest     []SkillFile
	SkillIDs          []string
	SuggestedSkillIDs []string
	ManifestSkillIDs  []string
	ReadOnly          bool
}

type AgentContextInput struct {
	Project         *models.AppProject
	Deps            *RunDependencies
	AttachmentsPool []models.AppAsset
	Message         string
	IsFirstGen      bool
}

func CompileAgentContext(input AgentContextInput) AgentContextPacket {
	profile := ResolveAgentProfile(input.Project)
	msgs := BuildSystemMessagesForProfile(profile, input.Project, input.Deps, input.AttachmentsPool, input.IsFirstGen)

	if addendum := SlashCommandSystemAddendum(input.Message); addendum != "" {
		msgs = append(msgs, utils.Message{
			Role:    "system",
			Content: addendum,
		})
	}

	skills := ResolveSkills(profile, input.Project, input.Message, input.IsFirstGen)
	suggestedSkills := ResolveSuggestedSkills(profile, input.Project, input.Message, input.IsFirstGen)
	manifestSkills := ResolveCompatibleSkills(profile, input.Project)
	if len(manifestSkills) > 0 {
		msgs = append(msgs, RenderSkillManifestMessage(manifestSkills, skills, suggestedSkills))
	}
	msgs = append(msgs, RenderSkillMessages(skills)...)

	return AgentContextPacket{
		Profile:           profile,
		SystemMessages:    msgs,
		Skills:            skills,
		RequiredSkills:    skills,
		SuggestedSkills:   suggestedSkills,
		SkillManifest:     manifestSkills,
		SkillIDs:          skillIDs(skills),
		SuggestedSkillIDs: skillIDs(suggestedSkills),
		ManifestSkillIDs:  skillIDs(manifestSkills),
		ReadOnly:          SlashCommandIsReadOnly(input.Message),
	}
}
