package agent

import (
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

type AgentContextPacket struct {
	Profile        *AgentProfile
	SystemMessages []utils.Message
	Skills         []SkillFile
	SkillIDs       []string
	ReadOnly       bool
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
	msgs = append(msgs, RenderSkillMessages(skills)...)

	return AgentContextPacket{
		Profile:        profile,
		SystemMessages: msgs,
		Skills:         skills,
		SkillIDs:       skillIDs(skills),
		ReadOnly:       SlashCommandIsReadOnly(input.Message),
	}
}
