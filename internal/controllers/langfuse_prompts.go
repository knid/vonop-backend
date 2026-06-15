package controllers

import (
	"context"
	"log/slog"

	"github.com/knid/wemob/internal/observability"
)

func (c *Controller) compileLangfuseTextPrompt(ctx context.Context, name, fallback string, vars map[string]string) observability.CompiledPrompt {
	client := observability.PromptClientFromEnvAndSecrets(c.SecretManager)
	compiled, err := client.CompileTextPrompt(ctx, name, vars)
	if err != nil || compiled.Text == "" {
		if err != nil {
			slog.Warn("failed to compile Langfuse prompt, using local fallback", "error", err, "prompt", name)
		}
		return observability.CompiledPrompt{Name: name, Text: fallback}
	}
	return compiled
}
