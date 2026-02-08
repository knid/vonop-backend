package integrations

import (
	"fmt"

	"github.com/knid/wemob/internal/constants"
)

// GetProviderConfig returns the configuration for a given integration provider
func GetProviderConfig(provider string) (*constants.IntegrationProviderConfig, error) {
	for _, p := range constants.IntegrationProviders {
		if p.Provider == provider {
			return &p, nil
		}
	}
	return nil, fmt.Errorf("unknown integration provider: %s", provider)
}

// ValidateCredentials checks that all required credential fields are present
func ValidateCredentials(provider string, credentials map[string]any) error {
	config, err := GetProviderConfig(provider)
	if err != nil {
		return err
	}

	for _, field := range config.RequiredFields {
		val, ok := credentials[field]
		if !ok {
			return fmt.Errorf("missing required field: %s", field)
		}
		strVal, ok := val.(string)
		if !ok || strVal == "" {
			return fmt.Errorf("field %s must be a non-empty string", field)
		}
	}

	return nil
}

// BuildEnvVars converts integration credentials to Vercel environment variables
func BuildEnvVars(provider string, credentials map[string]any) (map[string]string, error) {
	config, err := GetProviderConfig(provider)
	if err != nil {
		return nil, err
	}

	envVars := make(map[string]string)
	for credKey, envKey := range config.EnvVarMapping {
		if val, ok := credentials[credKey]; ok {
			if strVal, ok := val.(string); ok && strVal != "" {
				envVars[envKey] = strVal
			}
		}
	}

	return envVars, nil
}

// CollectEnvVarsFromIntegrations gathers all env vars from a list of integrations
func CollectEnvVarsFromIntegrations(integrations []struct {
	Provider    string
	Credentials map[string]any
}) (map[string]string, error) {
	allEnvVars := make(map[string]string)

	for _, integ := range integrations {
		envVars, err := BuildEnvVars(integ.Provider, integ.Credentials)
		if err != nil {
			return nil, fmt.Errorf("integration %s: %w", integ.Provider, err)
		}
		for k, v := range envVars {
			allEnvVars[k] = v
		}
	}

	return allEnvVars, nil
}
