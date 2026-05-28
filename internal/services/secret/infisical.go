package secret

import (
	"log/slog"
	"sync"

	infisical "github.com/infisical/go-sdk"
)

type InfisicalSecretManager struct {
	Client      *infisical.InfisicalClient
	projectID   string
	environment string
	cache       map[string]string
	mu          sync.RWMutex
}

func NewInfisicalSecretManager(client *infisical.InfisicalClient, projectID string, environment string) *InfisicalSecretManager {
	return &InfisicalSecretManager{
		Client:      client,
		projectID:   projectID,
		environment: environment,
		cache:       make(map[string]string),
	}
}

// WarmCache fetches every secret for the configured project/environment in a
// single ListSecrets API call and stores them in an in-memory map. Call once
// at startup; GetSecret reads from the cache after warming.
func (ism *InfisicalSecretManager) WarmCache() {
	secrets, err := ism.Client.Secrets().List(infisical.ListSecretsOptions{
		Environment:   ism.environment,
		ProjectID:     ism.projectID,
		SecretPath:    "/",
		IncludeImports: false,
	})
	if err != nil {
		slog.Error("infisical: failed to list secrets for cache warm", "error", err)
		return
	}

	ism.mu.Lock()
	for _, s := range secrets {
		ism.cache[s.SecretKey] = s.SecretValue
	}
	ism.mu.Unlock()

	slog.Info("infisical: secret cache warmed", "count", len(secrets))
}

func (ism *InfisicalSecretManager) GetSecret(key string) string {
	ism.mu.RLock()
	if v, ok := ism.cache[key]; ok {
		ism.mu.RUnlock()
		return v
	}
	ism.mu.RUnlock()

	// Cache miss — fetch live (should not happen after WarmCache).
	val, err := ism.fetch(key)
	if err != nil {
		slog.Error("infisical: secret fetch failed (cache miss)",
			"key", key,
			"environment", ism.environment,
			"error", err,
		)
		return ""
	}
	ism.mu.Lock()
	ism.cache[key] = val
	ism.mu.Unlock()
	return val
}

func (ism *InfisicalSecretManager) fetch(key string) (string, error) {
	secret, err := ism.Client.Secrets().Retrieve(infisical.RetrieveSecretOptions{
		SecretKey:   key,
		Environment: ism.environment,
		ProjectID:   ism.projectID,
		SecretPath:  "/",
	})
	if err != nil {
		return "", err
	}
	return secret.SecretValue, nil
}

func (ism *InfisicalSecretManager) GetEnv() string {
	return ism.environment
}
