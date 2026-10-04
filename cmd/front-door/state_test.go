package main

import (
	"encoding/json"
	"os"
	"testing"
)

func loadStatus(t *testing.T, name string) Status {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var s Status
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPageStateFromFixtures(t *testing.T) {
	cases := []struct {
		file   string
		caller string
		want   PageState
	}{
		{"status-running.json", "alice@example.com", StateReady},
		{"status-needslogin-url.json", "alice@example.com", StateClaim},
		{"status-needslogin.json", "alice@example.com", StateLogin},
		{"status-running.json", "mallory@example.com", StateConflict},
	}
	for _, c := range cases {
		if got := PageStateFor(c.caller, loadStatus(t, c.file)); got != c.want {
			t.Errorf("%s as %s = %s, want %s", c.file, c.caller, got, c.want)
		}
	}
}

func TestPageState(t *testing.T) {
	const alice = "alice@example.com"
	cases := []struct {
		name string
		s    Status
		want PageState
	}{
		{"no machine", Status{Name: "alice"}, StateNone},
		{"no owner recorded", Status{Name: "alice", Exists: true}, StateConflict},
		{"not reachable", Status{Name: "alice", Exists: true, Owner: alice, Running: true}, StateBooting},
		{"claimed by someone else", Status{Name: "alice", Exists: true, Owner: alice, Reachable: true,
			Tailscale: &TailscaleStatus{State: "Running", Owner: "bob@example.com"}}, StateWrongOwner},
		{"starting tailscale", Status{Name: "alice", Exists: true, Owner: alice, Reachable: true,
			Tailscale: &TailscaleStatus{State: "Starting"}}, StateBooting},
	}
	for _, c := range cases {
		if got := PageStateFor(alice, c.s); got != c.want {
			t.Errorf("%s = %s, want %s", c.name, got, c.want)
		}
	}
}
