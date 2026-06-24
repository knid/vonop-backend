package controllers

type fakeStaticSecretManager struct {
	values map[string]string
	env    string
}

func (f *fakeStaticSecretManager) GetSecret(key string) string {
	if f == nil || f.values == nil {
		return ""
	}
	return f.values[key]
}

func (f *fakeStaticSecretManager) GetEnv() string {
	if f == nil {
		return ""
	}
	return f.env
}
