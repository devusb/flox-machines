package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/devusb/flox-machines/internal/machines"
)

type fakeCLI struct {
	status    machines.Status
	creates   []string
	logins    int
	createErr error
	loginErr  error
}

func (f *fakeCLI) Status(_ context.Context, name string) (machines.Status, error) {
	s := f.status
	s.Name = name
	return s, nil
}

func (f *fakeCLI) Create(_ context.Context, name, owner string) error {
	f.creates = append(f.creates, name+" "+owner)
	return f.createErr
}

func (f *fakeCLI) Login(_ context.Context, name string) error {
	f.logins++
	return f.loginErr
}

type headerIdentity struct{}

func (headerIdentity) Caller(r *http.Request) (string, bool) {
	login := r.Header.Get("X-Test-Login")
	return login, login != ""
}

const alice = "alice@example.com"

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTest(cli *fakeCLI) (http.Handler, *clock) {
	c := &clock{t: time.Unix(1_000_000, 0)}
	return NewServer(cli, headerIdentity{}, []byte("test-key"), c.now), c
}

func get(h http.Handler, login string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/", nil)
	if login != "" {
		r.Header.Set("X-Test-Login", login)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func post(h http.Handler, path, login, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(url.Values{"token": {token}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Test-Login", login)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

var tokenPattern = regexp.MustCompile(`name="token" value="([0-9a-f]+)"`)

func tokenFrom(t *testing.T, body string) string {
	t.Helper()
	m := tokenPattern.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no token in page:\n%s", body)
	}
	return m[1]
}

func TestUnidentifiedCallerIsRefused(t *testing.T) {
	cli := &fakeCLI{}
	h, _ := newTest(cli)
	if w := get(h, ""); w.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", w.Code)
	}
	if w := post(h, "/create", "", "x"); w.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", w.Code)
	}
	if len(cli.creates) != 0 || cli.logins != 0 {
		t.Fatal("CLI was called for an unidentified caller")
	}
}

func TestReservedNameIsRefused(t *testing.T) {
	cli := &fakeCLI{}
	h, _ := newTest(cli)
	w := get(h, "root@example.com")
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "reserved") {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}
}

func TestCreate(t *testing.T) {
	cli := &fakeCLI{}
	h, _ := newTest(cli)
	page := get(h, alice)
	if !strings.Contains(page.Body.String(), "Create") {
		t.Fatalf("no create button:\n%s", page.Body.String())
	}
	token := tokenFrom(t, page.Body.String())

	if w := post(h, "/create", alice, "bad"); w.Code != http.StatusForbidden {
		t.Fatalf("bad token got %d, want 403", w.Code)
	}
	if w := post(h, "/create", alice, ""); w.Code != http.StatusForbidden {
		t.Fatalf("missing token got %d, want 403", w.Code)
	}
	if len(cli.creates) != 0 {
		t.Fatal("created without a valid token")
	}

	w := post(h, "/create", alice, token)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("got %d, want 303", w.Code)
	}
	if len(cli.creates) != 1 || cli.creates[0] != "alice "+alice {
		t.Fatalf("creates = %v", cli.creates)
	}
}

func TestCreateWhenMachineExists(t *testing.T) {
	cli := &fakeCLI{status: machines.Status{Exists: true, Owner: alice}}
	h, _ := newTest(cli)
	fresh, _ := newTest(&fakeCLI{})
	token := tokenFrom(t, get(fresh, alice).Body.String())
	post(h, "/create", alice, token)
	if len(cli.creates) != 0 {
		t.Fatalf("creates = %v", cli.creates)
	}
}

func TestTokenIsPerCaller(t *testing.T) {
	cli := &fakeCLI{}
	h, _ := newTest(cli)
	token := tokenFrom(t, get(h, alice).Body.String())
	if w := post(h, "/create", "bob@example.com", token); w.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", w.Code)
	}
}

func TestOwnerConflict(t *testing.T) {
	cli := &fakeCLI{status: machines.Status{Exists: true, Owner: "alice@other.example", Reachable: true,
		Tailscale: &machines.TailscaleStatus{State: "NeedsLogin"}}}
	h, _ := newTest(cli)
	w := get(h, alice)
	if !strings.Contains(strings.ToLower(w.Body.String()), "conflict") {
		t.Fatalf("no conflict:\n%s", w.Body.String())
	}
	fresh, _ := newTest(&fakeCLI{})
	token := tokenFrom(t, get(fresh, alice).Body.String())
	post(h, "/create", alice, token)
	post(h, "/login", alice, token)
	if len(cli.creates) != 0 || cli.logins != 0 {
		t.Fatalf("acted on another owner's machine: creates=%v logins=%d", cli.creates, cli.logins)
	}
}

func TestLoginIsRateLimited(t *testing.T) {
	cli := &fakeCLI{status: machines.Status{Exists: true, Owner: alice, Reachable: true,
		Tailscale: &machines.TailscaleStatus{State: "NeedsLogin"}}}
	h, c := newTest(cli)
	get(h, alice)
	get(h, alice)
	if cli.logins != 1 {
		t.Fatalf("logins = %d, want 1", cli.logins)
	}
	c.t = c.t.Add(61 * time.Second)
	get(h, alice)
	if cli.logins != 2 {
		t.Fatalf("logins = %d, want 2", cli.logins)
	}
}

func TestLoginButton(t *testing.T) {
	cli := &fakeCLI{status: machines.Status{Exists: true, Owner: alice, Reachable: true,
		Tailscale: &machines.TailscaleStatus{State: "NeedsLogin", AuthURL: "https://login.tailscale.com/a/abc"}}}
	h, _ := newTest(cli)
	token := tokenFrom(t, get(h, alice).Body.String())
	if w := post(h, "/login", alice, token); w.Code != http.StatusSeeOther {
		t.Fatalf("got %d, want 303", w.Code)
	}
	if cli.logins != 1 {
		t.Fatalf("logins = %d, want 1", cli.logins)
	}
}

func TestPages(t *testing.T) {
	cases := []struct {
		name   string
		status machines.Status
		want   []string
	}{
		{"booting", machines.Status{Exists: true, Owner: alice, Running: true}, []string{"Starting your machine", `http-equiv="refresh"`}},
		{"login", machines.Status{Exists: true, Owner: alice, Reachable: true, Tailscale: &machines.TailscaleStatus{State: "NeedsLogin"}},
			[]string{"Preparing your Tailscale login", `http-equiv="refresh"`}},
		{"claim", machines.Status{Exists: true, Owner: alice, Reachable: true,
			Tailscale: &machines.TailscaleStatus{State: "NeedsLogin", AuthURL: "https://login.tailscale.com/a/abc"}},
			[]string{`href="https://login.tailscale.com/a/abc"`, `http-equiv="refresh"`}},
		{"ready", loadStatus(t, "status-running.json"), []string{"ssh alice@machine-alice.example.ts.net", "sudo tailscale serve"}},
		{"wrong owner", machines.Status{Exists: true, Owner: alice, Reachable: true,
			Tailscale: &machines.TailscaleStatus{State: "Running", DNSName: "machine-alice.example.ts.net", Owner: "bob@example.com"}},
			[]string{"bob@example.com"}},
	}
	for _, c := range cases {
		h, _ := newTest(&fakeCLI{status: c.status})
		body := get(h, alice).Body.String()
		for _, want := range c.want {
			if !strings.Contains(body, want) {
				t.Errorf("%s: page lacks %q:\n%s", c.name, want, body)
			}
		}
	}
}

func TestReadyPageDoesNotRefresh(t *testing.T) {
	h, _ := newTest(&fakeCLI{status: loadStatus(t, "status-running.json")})
	if strings.Contains(get(h, alice).Body.String(), `http-equiv="refresh"`) {
		t.Fatal("ready page refreshes")
	}
}

func TestCreateFailureIsShown(t *testing.T) {
	cli := &fakeCLI{createErr: errors.New("machine create: reserved name 'dnsmasq'")}
	h, _ := newTest(cli)
	token := tokenFrom(t, get(h, alice).Body.String())
	w := post(h, "/create", alice, token)
	if !strings.Contains(w.Body.String(), "reserved name") {
		t.Fatalf("got %d, error not shown:\n%s", w.Code, w.Body.String())
	}
}

func TestLoginFailureIsShown(t *testing.T) {
	cli := &fakeCLI{loginErr: errors.New("could not start a Tailscale login"), status: machines.Status{Exists: true, Owner: alice, Reachable: true,
		Tailscale: &machines.TailscaleStatus{State: "NeedsLogin", AuthURL: "https://login.tailscale.com/a/abc"}}}
	h, _ := newTest(cli)
	token := tokenFrom(t, get(h, alice).Body.String())
	w := post(h, "/login", alice, token)
	if !strings.Contains(w.Body.String(), "could not start a Tailscale login") {
		t.Fatalf("got %d, error not shown:\n%s", w.Code, w.Body.String())
	}
}
