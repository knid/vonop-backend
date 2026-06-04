package agent

import (
	"fmt"
	"strings"
)

const designFilePath = "DESIGN.md"

var requiredDesignSections = []string{
	"register",
	"design direction",
}

func isDesignFilePath(path string) bool {
	return strings.EqualFold(strings.TrimSpace(path), designFilePath)
}

func validateDesignBriefContent(content string) error {
	sections := markdownSections(content)
	var missing []string
	for _, section := range requiredDesignSections {
		if !sections[section] {
			missing = append(missing, section)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("DESIGN.md is missing required sections: %s", strings.Join(missing, ", "))
	}
	return nil
}

func markdownSections(content string) map[string]bool {
	sections := make(map[string]bool)
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		heading := strings.TrimLeft(trimmed, "#")
		heading = strings.TrimSpace(heading)
		heading = strings.Trim(heading, ":")
		heading = strings.ToLower(heading)
		if heading != "" {
			sections[heading] = true
		}
	}
	return sections
}
