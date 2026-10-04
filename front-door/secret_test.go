package main

import "testing"

func TestNodeSecret(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"tskey-client-abc", "tskey-client-abc?ephemeral=false&preauthorized=true"},
		{"tskey-client-abc?ephemeral=true", "tskey-client-abc?ephemeral=true"},
		{"tskey-auth-xyz", "tskey-auth-xyz"},
		{"  tskey-auth-xyz\n", "tskey-auth-xyz"},
		{"tskey-client-abc\n", "tskey-client-abc?ephemeral=false&preauthorized=true"},
	}
	for _, c := range cases {
		if got := NodeSecret(c.raw); got != c.want {
			t.Errorf("NodeSecret(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}
