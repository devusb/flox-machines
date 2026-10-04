package machines

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var reservedNames = map[string]bool{
	"admin":     true,
	"root":      true,
	"nobody":    true,
	"sshd":      true,
	"tailscale": true,
	"microvm":   true,
	"nixbld":    true,
}

func MachineName(login string) (string, error) {
	local, _, _ := strings.Cut(login, "@")
	local, _, _ = strings.Cut(strings.ToLower(local), "+")

	var b strings.Builder
	dash := false
	for _, r := range local {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	name := strings.Trim(b.String(), "-")
	if name == "" {
		return "", errors.New("login has no usable name")
	}
	if name[0] >= '0' && name[0] <= '9' {
		name = "u-" + name
	}
	if len(name) > 31 {
		name = strings.TrimRight(name[:31], "-")
	}
	if reservedNames[name] {
		return "", fmt.Errorf("%q is a reserved name", name)
	}
	return name, nil
}

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)

func ValidName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("invalid name '%s'", name)
	}
	return nil
}
