# Go machine CLI and shared base: design

The `machine` CLI and the front door become one Go module. Both call the same machine primitives. The CLI runs them as root. The front door runs them in-process as its own unprivileged user, with no sudo.

## Goals

- One implementation of each machine operation, used by both the CLI and the front door.
- `machine` keeps its current subcommands, flags, output and exit codes. Existing NixOS tests pass unchanged.
- The front door runs as `flox-machines-front-door` without sudo. Host grants limit it to creating machines, starting their units, and reaching guests over SSH.
- The bash CLI (`pkgs/machine-cli.nix`) and the jq filter (`pkgs/tailscale-status.jq`) are removed.

## Non-goals

- Moving microvm.nix logic into Go. The CLI keeps calling the fork's `microvm -c` and the `microvm@` units.
- Sparse zvols, new default sizes and `machine grow`. They follow this work.
- vsock admin access and machinectl registration.

## Layout

| Path | Contents |
|---|---|
| `go.mod` | module `github.com/devusb/flox-machines` at the repo root |
| `internal/machines/` | primitives, config, names, status types, Tailscale status parsing, the runner interface |
| `cmd/machine/` | the root CLI |
| `cmd/front-door/` | the front door server, from today's `front-door/` |
| `pkgs/flox-machines.nix` | one `buildGoModule` package producing `machine` and `flox-machines-front-door` |

The `front-door/` directory, `pkgs/front-door.nix`, `pkgs/machine-cli.nix` and `pkgs/tailscale-status.jq` are removed. The flake exposes the package as `packages.x86_64-linux.flox-machines`.

## Configuration

The host module writes `/etc/flox-machines/config.json`. Both binaries read it at start.

| Field | Source |
|---|---|
| `stateDir` | `/var/lib/microvms` |
| `storage` | `floxMachines.storage` |
| `parentDataset` | `floxMachines.zfs.parentDataset` |
| `persistSize` | `floxMachines.defaults.persistSize`, in MB |
| `keyPath` | `/var/lib/flox-machines/id_ed25519` |
| `reservedNames` | the fixed list in today's CLI plus the template's users |

`MACHINE_CONFIG` overrides the path, for tests.

## Primitives

`internal/machines` exposes a `Manager` built from the config and a `Runner`:

| Method | Behavior |
|---|---|
| `Create(ctx, name, owner)` | Today's `cmd_create`. Takes a per-name lock. On failure, removes the instance directory and zvol it made. |
| `Status(ctx, name)` | Today's `status --json` fields, from `systemctl is-active` and `tailscale status --json` over SSH |
| `Login(ctx, name)` | Today's `cmd_login` |
| `SSH(name, args)` | Replaces the process with `ssh` as root on the machine |
| `Restart`, `Resize`, `ResizeReset`, `Reimage`, `Destroy`, `List`, `GC` | today's commands |
| `CheckName(name)` | name pattern, reserved list, and system UIDs below 1000 |
| `MachineName(login)` | today's front door login-to-name mapping |

`Runner` runs an external command with a context and returns stdout, or an error carrying its stderr. The real runner uses `os/exec`. Tests use a fake that records commands and returns canned output.

Create formats the persist zvol with `mkfs.ext4 -q -E root_owner=0:0 -L persist`. Without `root_owner`, a create by the front door makes the guest's `/persist` owned by the front door's UID.

Create takes an exclusive `flock` on `<stateDir>/.lock-<name>` for its whole run. The CLI and the front door both take it, so two creates of one name cannot interleave.

## CLI

`cmd/machine` parses arguments and calls the primitives. Usage text, output lines (`created machine-alice`, `destroyed machine-alice`, the status JSON) and error messages (`machine: <message>`, exit 1) match the bash script.

## Front door

`cmd/front-door` keeps its routes, page states, form tokens, rate limit and tsnet setup. Its `CLI` interface is satisfied by `machines.Manager` directly, and the `-machine` flag is removed.

## Host permissions

The front door unit runs as `flox-machines-front-door` with `kvm` as a supplementary group.

| Need | Grant |
|---|---|
| State directory and machine directories | `kvm` group; the fork already makes them `kvm`-writable |
| Persist zvol devices | `kvm` group, through the existing udev rule |
| `microvm -c` gcroots | `systemd.tmpfiles.rules`: `d /nix/var/nix/gcroots/microvm 0775 root kvm -` |
| Starting machines | `security.polkit.extraConfig`: `org.freedesktop.systemd1.manage-units`, verb `start`, units matching `microvm@machine-[a-z0-9-]+.service`, user `flox-machines-front-door` |
| ZFS | `ExecStartPre = "+zfs allow flox-machines-front-door create,mount,volsize,userprop <parentDataset>"` and the matching `zfs unallow` in `ExecStopPost`, when storage is zfs |
| Admin SSH key | `flox-machines-key` makes the key owned by `flox-machines-front-door`, mode 0600, and the key directory group `kvm`, mode 0750, when the front door is enabled |

Root's `machine` CLI can still use the key, because OpenSSH rejects only keys that are group- or world-readable and owned by the user running `ssh`.

The unit sets `ProtectSystem=strict`, `ReadWritePaths` for the state directory, the gcroots directory and its own state directory, `DeviceAllow` for `/dev/zfs` and `block-zd`, an empty `CapabilityBoundingSet`, `NoNewPrivileges`, and `RestrictAddressFamilies` for `AF_UNIX`, `AF_INET`, `AF_INET6` and `AF_NETLINK`.

The sudo rules and the `front-door-machine` wrapper are removed.

## Errors

- A failing external command returns an error that includes its stderr, as the bash CLI prints today.
- Create removes what it made when a later step fails: the zvol, then the instance directory and its gcroots.
- Primitives never prompt. polkit and ZFS refusals surface as errors.

## Testing

- Go unit tests:
  - `CheckName` and `MachineName`.
  - Status parsing, using the existing fixtures.
  - The command sequence of each primitive through the fake runner, including cleanup after a failed create.
- The `tailscale-status-jq` check is replaced by the Go unit tests, which run in the package build.
- NixOS tests pass unchanged, with `create-restart`, `zfs-backend` and `front-door` as the main regression set.
- `front-door` runs with the ZFS backend and additionally checks, as the front door user:
  - `sudo -n true` fails.
  - Stopping a machine unit is refused.
  - `zfs destroy` of a machine zvol is refused.

## Order of work

Each step leaves the suite passing:

1. Move the Go module to the repo root, with `cmd/front-door` unchanged in behavior.
2. Add `internal/machines` and `cmd/machine`. Switch the host module to the Go `machine`. Remove the bash CLI and jq filter.
3. Switch the front door to in-process calls. Add the host permissions. Remove sudo.
4. Update `README.md`, `AGENTS.md`, `docs/testing.md` and `docs/decisions.md`.
