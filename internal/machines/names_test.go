package machines

import (
	"strings"
	"testing"
)

func TestMachineName(t *testing.T) {
	cases := []struct {
		login string
		want  string
	}{
		{"alice@flox.dev", "alice"},
		{"First.Last@flox.dev", "first-last"},
		{"a.b..c@example.com", "a-b-c"},
		{"alice+dev@example.com", "alice"},
		{"-x-@example.com", "x"},
		{"9lives@example.com", "u-9lives"},
		{strings.Repeat("a", 40) + "@example.com", strings.Repeat("a", 31)},
		{strings.Repeat("a", 30) + ".b@example.com", strings.Repeat("a", 30)},
	}
	for _, c := range cases {
		got, err := MachineName(c.login)
		if err != nil {
			t.Errorf("MachineName(%q) error: %v", c.login, err)
			continue
		}
		if got != c.want {
			t.Errorf("MachineName(%q) = %q, want %q", c.login, got, c.want)
		}
	}
}

func TestMachineNameRefused(t *testing.T) {
	for _, login := range []string{"@example.com", "...@example.com", "root@example.com", "admin@example.com", "sshd@example.com", "nobody@example.com"} {
		if got, err := MachineName(login); err == nil {
			t.Errorf("MachineName(%q) = %q, want an error", login, got)
		}
	}
}

func TestValidName(t *testing.T) {
	for _, name := range []string{"alice", "a-1", "a" + strings.Repeat("b", 30)} {
		if err := ValidName(name); err != nil {
			t.Errorf("ValidName(%q) = %v, want nil", name, err)
		}
	}
	for _, name := range []string{"", "1a", "Alice", "a_b", "a" + strings.Repeat("b", 31)} {
		err := ValidName(name)
		if err == nil || err.Error() != "invalid name '"+name+"'" {
			t.Errorf("ValidName(%q) = %v, want invalid name", name, err)
		}
	}
}
