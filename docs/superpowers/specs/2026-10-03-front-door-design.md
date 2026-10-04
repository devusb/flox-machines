# Front door: design

A web service on the host that lets any tailnet member create their own machine and claim it onto their tailnet. This is the first service PR: create and claim only.

## Goals

- A tailnet member opens the page, taps Create, and gets a machine named after their login.
- The page shows the Tailscale login link for the new machine. Tapping it makes the machine the person's tailnet node.
- After the claim, the page shows the machine's tailnet name and the SSH command.
- One machine per person. The page never acts on any machine but the caller's own.

## Non-goals

- Restart, reimage or destroy from the page.
- An admin view of all machines.
- Disabling key expiry or deleting devices through the Tailscale API.
- The per-machine UDP port for direct connections.

## Architecture

```
person's browser ──tailnet──► front door (tsnet node "machines")
                                   │  WhoIs(remote addr) → login
                                   │  name = clean(localpart(login))
                                   ▼
                              machine CLI (root)
                                   │  create / status --json / login
                                   ▼
                     machine-<name> microVM ──► tailscaled (NeedsLogin, AuthURL after machine login)
```

The service handles HTTP and identity only. Every host operation goes through the `machine` CLI.

## Service

- Go, using `tailscale.com/tsnet`. One binary, `flox-machines-front-door`.
- Joins the tailnet as its own tagged node, with hostname `floxMachines.frontDoor.hostname` (default `machines`) and tags `floxMachines.frontDoor.tags` (default `[ "tag:flox-machines" ]`). It authenticates with an OAuth client secret read from `floxMachines.frontDoor.oauthSecretFile`, passed to tsnet as `ClientSecret` along with the tags. tsnet mints a node auth key from it. Keys minted from an OAuth secret are ephemeral unless the secret carries `?ephemeral=false`, so the service appends `?ephemeral=false&preauthorized=true` to a `tskey-client-` secret that has no attributes. A plain auth key in that file also works. With no file, tsnet prints a login URL to the service's log, and so to the journal, until an admin opens it. tsnet state lives in `/var/lib/flox-machines/front-door`.
- Listens with TLS on port 443 when the tailnet offers certificates for the node, and on plain HTTP port 80 otherwise.
- For each request, calls the tsnet local client's WhoIs with the remote address. Requests whose caller cannot be identified, and requests from tagged nodes, get 403. Only people create machines.
- Runs as root, because the CLI does.

### Machine names

The name comes from the caller's login, never from the request:

1. Take the part before `@`, lowercase it, and drop anything from a `+` onward.
2. Replace every run of characters outside `[a-z0-9]` with one `-`, and trim `-` from both ends. `first.last` becomes `first-last`.
3. If it starts with a digit, prefix `u-`.
4. Cut it to 31 characters and trim a trailing `-`.

A login that cleans to nothing, or to a reserved name, gets an error page and no machine. Reserved names are those of system accounts in the guest and on the host, plus `admin`, `root`, `nobody`, `sshd`, `tailscale`, `microvm` and `nixbld`.

Logins in this tailnet are unique, so two people mapping to one name is not expected. The CLI still records the owner's full login at create, in a host-only file `owner` in the instance directory outside the guest share. The front door acts on a machine only when its recorded owner equals the caller. A mismatch gets an error page naming the conflict, and nothing is created or changed.

### Routes

| Route | Behavior |
|---|---|
| `GET /` | Runs `machine status <name> --json`, checks the recorded owner, and renders one of the states below |
| `POST /create` | Runs `machine create <name> --owner <login>` if the caller has no machine, then redirects to `/`. If a machine exists, redirects to `/` without creating |
| `POST /login` | Runs `machine login <name>` and redirects to `/` |

`POST /create` and `POST /login` require a same-origin form token so a cross-site request cannot act for a visitor.

### Page states

| State | Condition | Page |
|---|---|---|
| none | status reports no machine | Create button |
| booting | machine exists, not yet reachable or tailscaled not yet reporting | "Starting your machine", page refreshes every 3 s |
| login | Tailscale state `NeedsLogin` with no login URL | The service runs `machine login <name>` once per minute at most, shows "Preparing your Tailscale login" and refreshes every 3 s. A button posts to `/login` to ask again |
| claim | Tailscale state `NeedsLogin` with a login URL | Link to the login URL, a button that posts to `/login` for a fresh link, and a refresh every 3 s |
| ready | Tailscale state `Running` and the node owner is the caller | Tailnet name, `ssh <name>@<tailnet name>`, and a note that `sudo tailscale serve` publishes services |
| wrong owner | Tailscale state `Running` and the node owner is a different login | A warning that the machine joined the tailnet as that login, with the ready details |
| conflict | the name is reserved, or the machine's recorded owner is a different login | An error naming the problem; nothing is created or changed |
| error | CLI failure | The CLI's error message |

## CLI: `machine create --owner`

`machine create <name> --owner <login>` writes `<login>` to `owner` in the instance directory. `machine status --json` reports it as `owner` at the top level. Machines created without `--owner` have none, and the front door treats them as belonging to nobody.

## CLI: `machine status`

`machine status <name> --json` prints one JSON object:

```json
{
  "name": "alice",
  "exists": true,
  "owner": "alice@flox.dev",
  "running": true,
  "reachable": true,
  "tailscale": {
    "state": "NeedsLogin",
    "authURL": "https://login.tailscale.com/a/...",
    "dnsName": "",
    "owner": ""
  }
}
```

- `exists` is false when there is no instance directory; every other field is then omitted.
- `running` is whether `microvm@machine-<name>` is active.
- `reachable` is whether `machine ssh` succeeded within 5 seconds.
- `tailscale` comes from `tailscale status --json` in the guest: `BackendState`, `AuthURL`, `Self.DNSName` with the trailing dot removed, and `owner`, the `LoginName` of `User[Self.UserID]`. It is omitted when the machine is not reachable.

## CLI: `machine login`

`machine login <name>` starts `tailscale up --ssh` in the background in the guest, detached from the SSH session, so tailscaled requests a login URL from the control server. It returns immediately. If the node is already logged in, it does nothing. It covers the first claim and any later re-login, such as after node key expiry.

## Guest: claim

The guest needs no login service. tailscaled starts with no key and waits in `NeedsLogin`. The front door asks for a login URL with `machine login` when the owner opens the page. The node's state survives restarts, so a claimed machine stays claimed until its key expires or it is logged out, and then the page offers a new link the same way.

The guest reaches the Tailscale control server through the host's NAT, so `floxMachines.bridge.externalInterface` must be set. The tailnet does not require device approval, so a confirmed login adds the machine directly. Tailscale SSH to the machine depends on the tailnet policy allowing members to SSH to their own devices as their own user.

## Host module options

| Option | Default | Meaning |
|---|---|---|
| `floxMachines.frontDoor.enable` | `false` | Run the front door |
| `floxMachines.frontDoor.hostname` | `"machines"` | tsnet node name |
| `floxMachines.frontDoor.tags` | `[ "tag:flox-machines" ]` | Tags the front door node advertises |
| `floxMachines.frontDoor.oauthSecretFile` | `null` | OAuth client secret, or an auth key, for the front door's join. Without it, tsnet prints a login URL to the journal |

The service is a systemd unit `flox-machines-front-door` with `machine` on its `PATH`.

## Testing

- Go unit tests: OAuth secret attributes are added when missing and kept when present; name cleaning including `first.last`, `+` tags, leading digits and reserved names; owner match and conflict; state selection from status JSON including the login, claim and wrong-owner states, handlers against a fake CLI, form token check.
- NixOS test `front-door`: the service runs with a test-only flag that listens on localhost over plain HTTP and takes the caller's login from an `X-Test-Login` header instead of WhoIs. The flag is never set by the module. The test creates a machine for `alice@example.com`, checks a second create does not make another machine, checks a header-less request gets 403, and checks the page reaches the login state: `tailscale.state` is `NeedsLogin` with no URL, because the test network cannot reach Tailscale. It also checks `machine login` returns without error.
- The real login URL and claim are checked on a live host.

## Tailnet policy

- `tag:flox-machines` is defined, owned by admins, and the OAuth client may create keys for it.
- A grant lets members reach `tag:flox-machines` on port 443, and on port 80 if HTTPS certificates are off.
- A Tailscale SSH rule lets members SSH to their own devices as their own user.
- Device approval is not required.
