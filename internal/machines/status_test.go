package machines

import (
	"encoding/json"
	"os"
	"testing"
)

func TestParseTailscaleStatusFixtures(t *testing.T) {
	cases := map[string]TailscaleStatus{
		"tailscale-running":        {State: "Running", DNSName: "machine-alice.example.ts.net", Owner: "alice@example.com"},
		"tailscale-needslogin-url": {State: "NeedsLogin", AuthURL: "https://login.tailscale.com/a/abc123"},
		"tailscale-needslogin":     {State: "NeedsLogin"},
	}
	for name, want := range cases {
		raw, err := os.ReadFile("testdata/" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		got, err := ParseTailscaleStatus(raw)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != want {
			t.Errorf("%s: got %+v, want %+v", name, got, want)
		}
	}
}

func TestStatusJSONMissing(t *testing.T) {
	out, err := json.Marshal(Status{Name: "x", Owner: "ignored", Running: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"name":"x","exists":false}` {
		t.Errorf("got %s", out)
	}
}

func TestStatusJSONRunning(t *testing.T) {
	out, err := json.Marshal(Status{Name: "a", Exists: true, Owner: "o", Running: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"name":"a","exists":true,"owner":"o","running":true,"reachable":false}` {
		t.Errorf("got %s", out)
	}
	out, err = json.Marshal(Status{Name: "a", Exists: true, Reachable: true, Tailscale: &TailscaleStatus{State: "Running"}})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"name":"a","exists":true,"owner":"","running":false,"reachable":true,"tailscale":{"state":"Running","authURL":"","dnsName":"","owner":""}}`
	if string(out) != want {
		t.Errorf("got %s", out)
	}
}

func TestStatusJSONRoundTrip(t *testing.T) {
	in := Status{Name: "a", Exists: true, Owner: "o", Reachable: true, Tailscale: &TailscaleStatus{State: "NeedsLogin"}}
	out, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var back Status
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if back.Name != in.Name || back.Owner != in.Owner || !back.Exists || back.Tailscale == nil || back.Tailscale.State != "NeedsLogin" {
		t.Errorf("round trip got %+v", back)
	}
}
