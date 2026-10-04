# Testing

Tests are NixOS tests that run machines as nested microVMs. They are x86_64-linux only.

```bash
nix build -L --no-link --eval-store auto --store ssh-ng://<builder> \
  .#checks.x86_64-linux.create-restart .#checks.x86_64-linux.store-reboot \
  .#checks.x86_64-linux.zfs-backend .#checks.x86_64-linux.user-units
```

The fork's tests are exposed through this flake and run against a local checkout of the fork:

```bash
nix build -L --no-link --eval-store auto --store ssh-ng://<builder> \
  --override-input microvm git+file:///path/to/microvm.nix \
  .#legacyPackages.x86_64-linux.forkTests.instances \
  .#legacyPackages.x86_64-linux.forkTests.instances-restart \
  .#legacyPackages.x86_64-linux.forkTests.overlay-store
```

| Test | Covers |
|---|---|
| `create-restart` | `machine` create, ssh, resize, destroy, name checks; owner account with sudo; pinned registry; flox and home-manager installed; tailscaled running with Tailscale SSH on, kept across a base update; restart onto a new base with home, Tailscale state and the SSH host key kept |
| `store-reboot` | guest-added store paths and host paths across restart, base update, `machine gc` and reimage |
| `user-units` | an enabled user unit starts after a machine restart with nobody logged in, and the journal keeps the previous boot |
| `zfs-backend` | the persistent zvol is created, keeps data across a restart, and is destroyed with the machine |
| `forkTests.instances` | template instances, `instance.env`, late-bound memory, vCPUs, taps, MACs, per-instance machine-id |
| `forkTests.instances-restart` | relink on host switch, no-op switch restarts nothing, template sizing flows to instances without an override |
| `forkTests.overlay-store` | overlay store: host paths visible, guest paths persist, repair of a deleted host path from a substituter |

The test network has no internet access. Test guests use `checks/lean-guest.nix`, which disables substituters, and every guest command runs under a timeout.

## Not covered by tests

These need network access and are checked on a live host:

- Running flox: installing, activating or pulling environments.
- home-manager activation, which builds a user environment.
- `nix registry list`, which fetches the global flake registry.
- Repair of missing host paths from cache.nixos.org at boot.
- Store behavior for closures a person pulls after the machine starts, including flox and home-manager closures across restarts, base updates and host garbage collection.
- User-level Nix commands run through `su -` sessions.
- Joining the tailnet, Tailscale SSH logins and `tailscale serve`.
