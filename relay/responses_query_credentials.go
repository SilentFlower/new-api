package relay

import "strings"

func isResponsesClientQueryCredentialKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	switch normalized {
	case "key",
		"api_key",
		"api-key",
		"apikey",
		"x-api-key",
		"access_token",
		"refresh_token",
		"id_token",
		"token",
		"authorization",
		"auth",
		"client_secret",
		"secret",
		"password",
		"passwd",
		"signature",
		"sig",
		"awsaccesskeyid",
		"x-amz-credential",
		"x-amz-security-token",
		"x-amz-signature":
		return true
	}
	return strings.Contains(normalized, "token") ||
		strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "signature")
}
