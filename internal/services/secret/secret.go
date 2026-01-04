package secret

type SecretManager interface {
	GetSecret(key string) string
	GetEnv() string
}
