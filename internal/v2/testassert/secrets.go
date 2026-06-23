package testassert

import (
	"strings"
	"testing"
)

const (
	SecretFileContent            = "not-a-real-secret-token"
	SensitiveFileContent         = "not-a-real-preview-secret"
	SensitiveComponentInputValue = "example-secret-token"
	SensitiveServiceEnvironment  = "not-a-real-service-token"
	SensitiveVariableDefault     = "not-a-real-variable-secret"
)

func NoSecretLeak(t *testing.T, label, text string, allowed ...string) {
	t.Helper()

	allowedSet := map[string]bool{}
	for _, value := range allowed {
		allowedSet[value] = true
	}
	for _, secret := range []string{
		SecretFileContent,
		SensitiveFileContent,
		SensitiveComponentInputValue,
		SensitiveServiceEnvironment,
		SensitiveVariableDefault,
	} {
		if secret == "" || allowedSet[secret] {
			continue
		}
		if strings.Contains(text, secret) {
			t.Fatalf("%s leaked sensitive value %q:\n%s", label, secret, text)
		}
	}
}
