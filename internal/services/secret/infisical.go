package secret

import (
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
		return ""
	}
	return secret.SecretValue
}

func (ism *InfisicalSecretManager) GetEnv() string {
	return ism.environment
}
