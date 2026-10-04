# Front Door Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A tsnet web service on the host that lets a tailnet member create their own machine and claim it onto their tailnet.

**Architecture:** A Go binary joins the tailnet as a tagged tsnet node, identifies each caller with WhoIs, derives a machine name from the login, and shells out to the `machine` CLI for create, status and login. The CLI gains `--owner`, `status --json` and `login`. The host module gains `floxMachines.frontDoor.*` options and a systemd unit.

**Tech Stack:** Go 1.25+, `tailscale.com/tsnet`, `html/template`, Nix `buildGoModule`, NixOS tests on chopper.

**Spec:** `docs/superpowers/specs/2026-10-03-front-door-design.md`

## Global Constraints

- Go module at `front-door/`, module path `github.com/devusb/flox-machines/front-door`, binary `flox-machines-front-door`.
- `tailscale.com` at a version that includes `feature/oauthkey` and `tsnet.Server.ClientSecret`; use the latest release.
- No network access in NixOS tests. Every guest command in tests runs under `timeout`.
- Test builds: `nix build -L --no-link --eval-store auto --store ssh-ng://mhelton@chopper <installable>`. Go unit tests run locally with `nix shell nixpkgs#go --command go test ./...` in `front-door/`.
- Name rules, reserved names, page states and JSON fields are exactly as in the spec.
- Commit style per the `writing-commits` skill. Work happens in `.worktrees/front-door` on branch `front-door`.

## Review Focus

1. A request without an identifiable caller, or from a tagged node, must never reach the CLI. Pinned by the handler test for 403.
2. A caller must never act on a machine whose recorded owner is another login, including a machine created without an owner. Pinned by the owner-conflict handler test.
3. `machine login` must return immediately even though `tailscale up` blocks. Pinned by the NixOS test's `timeout 20` around it.
4. The page polls every 3 s; `machine login` must not run more than once a minute per machine from polling. Pinned by the rate-limit handler test.
5. A cross-site POST without the form token must not create a machine. Pinned by the token handler test.

---

### Task 1: CLI owner, status and login

**Files:**
- Modify: `pkgs/machine-cli.nix`
- Create: `pkgs/tailscale-status.jq` (the extraction filter the CLI runs on the guest's `tailscale status --json`)
- Create: `checks/fixtures/tailscale-running.json`, `checks/fixtures/tailscale-needslogin.json`, `checks/fixtures/tailscale-needslogin-url.json`
- Create: `checks/tailscale-status-jq.nix` (a plain derivation, no VM)
- Create: `checks/front-door.nix` (CLI part only in this task)
- Modify: `checks/default.nix`

**Interfaces:**
- Produces: `machine create <name> [--owner <login>]` writes `<login>` to `/var/lib/microvms/machine-<name>/owner` (host only, mode 0600, root-owned) before starting the machine.
- Produces: `machine status <name> --json` prints `{"name","exists","owner","running","reachable","tailscale":{"state","authURL","dnsName","owner"}}` per the spec. `exists:false` prints only `name` and `exists`. `owner` is `""` when no file. `tailscale` omitted when not reachable. `reachable` uses `timeout 5 machine ssh <name> true`. Tailscale fields come from `tailscale status --json` in the guest: `.BackendState`, `.AuthURL`, `.Self.DNSName` without trailing dot, `.User[(.Self.UserID|tostring)].LoginName`.
- Produces: `machine login <name>` runs, via ssh, `tailscale status --json` and returns 0 if `BackendState` is `Running`; otherwise `systemd-run --unit=machine-tailscale-login --collect tailscale up --ssh` in the guest (a transient unit, so it outlives the SSH session) and returns 0.
- Produces: `check_name` also refuses `admin root nobody sshd tailscale microvm nixbld` and any name `getent passwd` knows on the host.

- [ ] **Step 0: Capture fixtures.** `tailscale status --json` on this workstation gives the `Running` shape; spike 1's guest output gives `NeedsLogin` with `AuthURL`; a fresh tailscaled gives `NeedsLogin` without it. Reduce each to `BackendState`, `AuthURL`, `Self.DNSName`, `Self.UserID` and the matching `User` entry, and replace real names and addresses with `example.com` values. Write `checks/tailscale-status-jq.nix`: runs `jq -f pkgs/tailscale-status.jq` on each fixture and compares with expected objects (`{"state":"Running","authURL":"","dnsName":"machine-alice.example.ts.net","owner":"alice@example.com"}`, `{"state":"NeedsLogin","authURL":"https://login.tailscale.com/a/abc123","dnsName":"","owner":""}`, `{"state":"NeedsLogin","authURL":"","dnsName":"","owner":""}`). Build it, see it fail with no filter, write the filter, see it pass.

- [ ] **Step 1: Write the failing test** `checks/front-door.nix`, host as in `user-units.nix` (2 cores, lean guest, `persistSize = 512`, `storeSize = 2048`, `mem = 1024`, `vcpu = 1`). Script:

```python
host.wait_for_unit("multi-user.target")
host.succeed("machine create alice --owner alice@example.com")
host.succeed("test \"$(cat /var/lib/microvms/machine-alice/owner)\" = alice@example.com")
host.succeed("test \"$(stat -c %a /var/lib/microvms/machine-alice/owner)\" = 600")
host.wait_until_succeeds("timeout 10 machine ssh alice true", timeout=300)
import json
s = json.loads(host.succeed("timeout 30 machine status alice --json"))
assert s["exists"] and s["owner"] == "alice@example.com" and s["running"] and s["reachable"], s
assert s["tailscale"]["state"] == "NeedsLogin", s
host.succeed("timeout 20 machine login alice")
host.succeed("timeout 60 machine ssh alice systemctl is-active machine-tailscale-login.service || timeout 60 machine ssh alice systemctl is-failed machine-tailscale-login.service")
assert json.loads(host.succeed("timeout 30 machine status nobody-here --json")) == {"name": "nobody-here", "exists": False}
host.fail("machine create root")
host.fail("machine create admin")
```

Register `front-door` in `checks/default.nix`.

- [ ] **Step 2: Run it and see it fail** — `nix build ... .#checks.x86_64-linux.front-door`. Expected: `machine create alice --owner ...` fails (unknown argument).

- [ ] **Step 3: Implement** the three commands and the reserved check in `machine-cli.nix`. Add `jq` to `runtimeInputs`. `create` parses `--owner` after the name.

- [ ] **Step 4: Run it and see it pass.**

- [ ] **Step 5: Commit** — `feat(cli): add machine status, login and owner records`.

---

### Task 2: Go module, names and secrets

**Files:**
- Create: `front-door/go.mod`, `front-door/names.go`, `front-door/names_test.go`, `front-door/secret.go`, `front-door/secret_test.go`

**Interfaces:**
- Produces: `func MachineName(login string) (string, error)` — spec rules; error for empty or reserved. Reserved list as Task 1 plus names in `/etc/passwd` is NOT checked here (the CLI checks the host).
- Produces: `func NodeSecret(raw string) string` — trims whitespace; if it starts with `tskey-client-` and has no `?`, appends `?ephemeral=false&preauthorized=true`; otherwise returns it unchanged.

- [ ] **Step 1: Write failing tests.** `names_test.go` table: `alice@flox.dev→alice`, `First.Last@flox.dev→first-last`, `a.b..c@x→a-b-c`, `alice+dev@x→alice`, `-x-@x→x`, `9lives@x→u-9lives`, 40 `a`s `@x` → 31 `a`s, `"@x"→error`, `root@x→error`, `admin@x→error`, `sshd@x→error`. `secret_test.go`: `tskey-client-abc→tskey-client-abc?ephemeral=false&preauthorized=true`, `tskey-client-abc?ephemeral=true→unchanged`, `tskey-auth-xyz→unchanged`, `"  tskey-auth-xyz\n"→tskey-auth-xyz`.

- [ ] **Step 2: Run** `go test ./...` — fails to compile.

- [ ] **Step 3: Implement** both functions.

- [ ] **Step 4: Run** `go test ./...` — PASS.

- [ ] **Step 5: Commit** — `feat(front-door): add machine name and secret rules`.

---

### Task 3: Status and page state

**Files:**
- Create: `front-door/state.go`, `front-door/state_test.go`

**Interfaces:**
- Produces: `type Status struct` matching Task 1's JSON (`Name, Exists, Owner, Running, Reachable, Tailscale *TailscaleStatus{State, AuthURL, DNSName, Owner}`).
- Produces: `type PageState string` with constants `StateNone, StateBooting, StateLogin, StateClaim, StateReady, StateWrongOwner, StateConflict`.
- Produces: `func PageStateFor(caller string, s Status) PageState` — `!Exists→None`; `Owner != caller→Conflict`; `Tailscale==nil→Booting`; `NeedsLogin && AuthURL==""→Login`; `NeedsLogin→Claim`; `Running && Tailscale.Owner==caller→Ready`; `Running→WrongOwner`; otherwise `Booting`.

- [ ] **Step 1: Write failing tests** for each branch, including owner `""` → Conflict. The Running, Claim and Login cases decode `front-door/testdata/status-*.json`, built from Task 1's fixtures run through the jq filter and wrapped in the CLI's outer object.
- [ ] **Step 2: Run** — fails.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — PASS.
- [ ] **Step 5: Commit** — `feat(front-door): map machine status to page states`.

---

### Task 4: HTTP handlers

**Files:**
- Create: `front-door/server.go`, `front-door/server_test.go`, `front-door/templates/page.html`

**Interfaces:**
- Consumes: `MachineName`, `Status`, `PageStateFor`.
- Produces: `type CLI interface { Status(ctx, name) (Status, error); Create(ctx, name, owner string) error; Login(ctx, name string) error }` and `type ExecCLI struct{ Path string }` implementing it by running `machine`.
- Produces: `type Identity interface { Caller(r *http.Request) (login string, ok bool) }`.
- Produces: `func NewServer(cli CLI, id Identity, secret []byte, now func() time.Time) http.Handler` with routes `GET /`, `POST /create`, `POST /login`.
- Form token: HMAC-SHA256 of the caller login with `secret`, hex, in hidden field `token`; POSTs with a wrong or missing token get 403.
- `GET /` in state Login calls `cli.Login` at most once per 60 s per machine name, using `now`.
- Page refreshes with `<meta http-equiv="refresh" content="3">` in states Booting, Login, Claim.

- [ ] **Step 1: Write failing tests** with a fake CLI and fake identity: no identity → 403 and no CLI call; `GET /` none → page has Create form with token; `POST /create` with token → `Create(name, login)` called once, 303 to `/`; `POST /create` when status exists → no Create; `POST /create` bad token → 403; owner mismatch → Conflict page, no Create or Login; Login state twice within 60 s → one `Login` call, again after 61 s → second call; Claim page contains the AuthURL as a link; Ready page contains `ssh alice@<dnsName>`.
- [ ] **Step 2: Run** — fails.
- [ ] **Step 3: Implement** handlers and template.
- [ ] **Step 4: Run** — PASS.
- [ ] **Step 5: Commit** — `feat(front-door): add web handlers`.

---

### Task 5: tsnet main and Nix package

**Files:**
- Create: `front-door/main.go`, `pkgs/front-door.nix`
- Modify: `flake.nix` (`packages.x86_64-linux.front-door`)

**Interfaces:**
- Produces flags: `--hostname` (default `machines`), `--tags` (comma list, default `tag:flox-machines`), `--secret-file`, `--state-dir` (default `/var/lib/flox-machines/front-door`), `--machine` (path to CLI, default `machine`), `--test-listen` (address; when set, plain HTTP on that address, identity from `X-Test-Login`, no tsnet), `--form-key-file` (default `<state-dir>/form.key`, created with 32 random bytes if missing).
- tsnet identity: `LocalClient().WhoIs(ctx, r.RemoteAddr)`; refuse when `Node.IsTagged()` or `UserProfile` is nil; login is `UserProfile.LoginName`.
- TLS: `srv.ListenTLS("tcp", ":443")` when `LocalClient().Status` shows `CertDomains` non-empty, else `srv.Listen("tcp", ":80")`.
- `UserLogf` set to `log.Printf` so the login URL reaches the journal.

- [ ] **Step 1:** Write `main.go`; `go vet ./...` and `go build ./...` succeed.
- [ ] **Step 2:** Write `pkgs/front-door.nix` with `buildGoModule`, `subPackages = [ "." ]`, `src = ../front-door`. Set `vendorHash` from the first failing build's reported hash.
- [ ] **Step 3:** `nix build .#packages.x86_64-linux.front-door` locally succeeds and `result/bin/flox-machines-front-door --help` lists the flags.
- [ ] **Step 4: Commit** — `feat(front-door): add tsnet entry point and package`.

---

### Task 6: Host module and end-to-end test

**Files:**
- Modify: `modules/host.nix`, `checks/front-door.nix`, `README.md`, `docs/testing.md`

**Interfaces:**
- Produces options per spec: `floxMachines.frontDoor.{enable, hostname, tags, oauthSecretFile}` plus internal `floxMachines.frontDoor.testListen` (`nullOr str`, default `null`, documented as for tests only).
- Produces unit `flox-machines-front-door.service`: `wantedBy multi-user.target`, `after network-online.target`, `ExecStart` with the flags, `path` containing the `machine` CLI, `StateDirectory = "flox-machines/front-door"`, `Restart = "on-failure"`. `oauthSecretFile` passed via `LoadCredential` and `--secret-file $CREDENTIALS_DIRECTORY/secret`.

- [ ] **Step 1: Extend the failing test** with a smoke check only; handler behavior is covered by Task 4's unit tests. Host sets `floxMachines.frontDoor = { enable = true; testListen = "127.0.0.1:8080"; };`. Append:

```python
host.wait_for_unit("flox-machines-front-door.service")
host.wait_for_open_port(8080)
host.succeed("curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/ | grep -qx 403")
import re
page = host.succeed("curl -s -H 'X-Test-Login: bob@example.com' http://127.0.0.1:8080/")
token = re.search(r'name="token" value="([0-9a-f]+)"', page).group(1)
host.succeed(f"curl -s -o /dev/null -H 'X-Test-Login: bob@example.com' -d token={token} http://127.0.0.1:8080/create")
host.succeed("test \"$(cat /var/lib/microvms/machine-bob/owner)\" = bob@example.com")
```

- [ ] **Step 2: Run** — fails at `wait_for_unit` (no unit).
- [ ] **Step 3: Implement** the module options and unit.
- [ ] **Step 4: Run** `front-door` plus `create-restart` — PASS.
- [ ] **Step 5: Docs.** README: a "Front door" section with the options, the tailnet policy list from the spec, and the journal login fallback. `docs/testing.md`: a `front-door` row; add "the real front door join, login URLs and claims" to "Not covered by tests".
- [ ] **Step 6: Commit** — `feat: add front door service to the host module`.

---

### Task 7: Live iteration on the Hetzner host

This is a checkpoint with Morgan, not a pass/fail task. The branch is ready for real use, and the flow is tested and iterated on the real host until it is good. Host state may be wiped and redeployed as often as needed.

**Prerequisites from Morgan:** SSH access to the Hetzner host and whether NixOS is installed on it; the tailnet policy entries from the spec's "Tailnet policy" section; an OAuth client secret for `tag:flox-machines`, or approval of the front door's login URL from the journal.

- [ ] **Step 1:** Add a host configuration for the box to a deploy flake that consumes this branch, with `floxMachines.enable`, `storage = "zfs"`, `bridge.externalInterface` set to the public interface, and `frontDoor.enable`. Deploy with `nixos-rebuild switch --target-host`, or with nixos-anywhere if the box is not yet NixOS.
- [ ] **Step 2:** Confirm the front door joins the tailnet as `machines` with `tag:flox-machines`, and serves HTTPS.
- [ ] **Step 3:** Morgan opens the page, creates a machine, taps the claim link, and reaches the ready page. Check `ssh <name>@<tailnet name>` and a direct (not DERP) connection.
- [ ] **Step 4:** Iterate on whatever is rough: wording, timing, states, errors. Each change gets a unit test where it touches handler logic, then redeploys. Wipe and recreate machines and front door state as needed.
- [ ] **Step 5:** Commit what changed, one commit per fix.
