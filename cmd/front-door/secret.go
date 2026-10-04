package main

import "strings"

func NodeSecret(raw string) string {
	secret := strings.TrimSpace(raw)
	if strings.HasPrefix(secret, "tskey-client-") && !strings.Contains(secret, "?") {
		secret += "?ephemeral=false&preauthorized=true"
	}
	return secret
}
