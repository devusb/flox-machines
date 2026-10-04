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
- Joins the tailnet as its own node with hostname `floxMachines.frontDoor.hostname`, default `machines`. tsnet state lives in `/var/lib/flox-machines/front-door`.
- Listens with TLS on port 443 when the tailnet offers certificates for the node, and on plain HTTP port 80 otherwise.
- For each request, calls the tsnet local client's WhoIs with the remote address. Requests whose caller cannot be identified, and requests from tagged nodes, get 403.
- The machine name is the login's local part, lowercased, with every character outside `[a-z0-9-]` replaced by `-`, leading characters that are not letters removed, and the result cut to 31 characters. A login that cleans to nothing gets 403. The name is never read from the request.
- Runs as root, because the CLI does.

### Routes

| Route | Behavior |
|---|---|
| `GET /` | Runs `machine status <name> --json` and renders one of the states below |
| `POST /create` | Runs `machine create <name>` if the caller has no machine, then redirects to `/`. If a machine exists, redirects to `/` without creating |
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
| error | CLI failure | The CLI's error message |

## CLI: `machine status`

`machine status <name> --json` prints one JSON object:

```json
{
  "name": "alice",
  "exists": true,
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
| `floxMachines.frontDoor.authKeyFile` | `null` | Auth key for the service's own first join; without it, the login URL is printed to the journal |

The service is a systemd unit `flox-machines-front-door` with `machine` on its `PATH`.

## Testing

- Go unit tests: name cleaning, state selection from status JSON including the login, claim and wrong-owner states, handlers against a fake CLI, form token check.
- NixOS test `front-door`: the service runs with a test-only flag that listens on localhost over plain HTTP and takes the caller's login from an `X-Test-Login` header instead of WhoIs. The flag is never set by the module. The test creates a machine for `alice@example.com`, checks a second create does not make another machine, checks a header-less request gets 403, and checks the page reaches the login state: `tailscale.state` is `NeedsLogin` with no URL, because the test network cannot reach Tailscale. It also checks `machine login` returns without error.
- The real login URL and claim are checked on a live host.
