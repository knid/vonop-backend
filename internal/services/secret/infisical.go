package secret

import (
	"log/slog"

	infisical "github.com/infisical/go-sdk"
)

type InfisicalSecretManager struct {
	Client      *infisical.InfisicalClient
	projectID   string
	environment string
}

func NewInfisicalSecretManager(client *infisical.InfisicalClient, projectID string, environment string) *InfisicalSecretManager {
	return &InfisicalSecretManager{
		Client:      client,
		projectID:   projectID,
		environment: environment,
	}
}

func (ism *InfisicalSecretManager) GetSecret(key string) string {

	secret, err := ism.Client.Secrets().Retrieve(infisical.RetrieveSecretOptions{
		SecretKey:   key,
		Environment: ism.environment,
		ProjectID:   ism.projectID,
		SecretPath:  "/",
	})
	if err != nil {
		// Surface the Infisical fetch failure so silent empties are
		// visible in BetterStack. Returning "" is preserved for back-compat
		// with callers that treat empty as "feature disabled" (e.g.
		// optional services like REVENUECAT_API_KEY, CODEMAGIC_API_TOKEN).
		// Empty-on-failure for *required* secrets is caught downstream by
		// the bootstrap empty-check guards.
		slog.Error("infisical: secret fetch failed",
			"key", key,
			"environment", ism.environment,
			"error", err,
		)
		return ""
	}
	return secret.SecretValue
}

func (ism *InfisicalSecretManager) GetEnv() string {
	return ism.environment
}
