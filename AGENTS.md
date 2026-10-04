# Working on Flox Machines

Flox Machines turns one declared NixOS template into per-person microVMs on a host. Read `README.md` for what it does, `docs/superpowers/specs/` for designs, and `docs/decisions.md` for choices made along the way.

## Layout

| Path | Contents |
|---|---|
| `flake.nix` | inputs, `nixosModules.floxMachines`, `nixosModules.machineTemplate`, `packages.x86_64-linux.front-door`, `checks` including the fork's tests as `fork-*` |
| `modules/host.nix` | the `floxMachines` host module: template registration, bridge, DHCP, NAT, admin key, ZFS |
| `modules/template.nix` | the guest template; imports `user.nix`, `tailscale.nix`, `persist.nix` |
| `modules/user.nix` | owner account and linger, created at boot from the instance share |
| `modules/persist.nix` | the `/persist` volume and impermanence paths |
| `internal/machines/` | machine operations shared by the CLI and the front door: config, names, status, create, destroy and the rest |
| `cmd/machine/` | the `machine` CLI |
| `cmd/front-door/` | the front door service |
| `pkgs/flox-machines.nix` | the Nix package for both binaries; update `vendorHash` when Go dependencies change |
| `checks/` | NixOS tests; `lean-guest.nix` is shared by them |
| `docs/testing.md` | what each test covers and what is only checked on a live host |

## The microvm.nix fork

The `microvm` input is `github:devusb/microvm.nix/instances`, a fork that adds templates, instances, late-bound runner values, the instance share and the overlay store. Its own tests are `checks/instances.nix` and `checks/overlay-store.nix` in the fork. Changes to template or instance behavior usually belong in the fork; anything specific to Flox Machines belongs here.

To work on both at once, clone the fork next to this repo and point this flake at the checkout:

```bash
git clone -b instances git@github.com:devusb/microvm.nix.git ../microvm.nix
nix build ... --override-input microvm git+file://$PWD/../microvm.nix ...
```

After a fork change is pushed, run `nix flake update microvm` here.

## Running tests

Go unit tests run from the repo root and also run in the package build:

```bash
CGO_ENABLED=0 go test ./...
```

Tests are x86_64-linux NixOS tests that run machines as nested VMs, so the builder needs KVM. Build on a remote store so outputs stay on the builder:

```bash
nix build -L --no-link --eval-store auto --store ssh-ng://<builder> .#checks.x86_64-linux.create-restart
```

| Installable | Script time |
|---|---|
| `.#checks.x86_64-linux.create-restart` | about 360 s |
| `.#checks.x86_64-linux.store-reboot` | about 155 s |
| `.#checks.x86_64-linux.zfs-backend` | about 110 s |
| `.#checks.x86_64-linux.user-units` | about 65 s |
| `.#checks.x86_64-linux.front-door` | about 55 s |
| `.#checks.x86_64-linux.front-door-tsnet` | about 160 s |
| `.#checks.x86_64-linux.network-isolation` | about 50 s |
| `.#checks.x86_64-linux.host-gc-refused` | seconds, no VM |
| `.#checks.x86_64-linux.fork-instances` | about 65 s |
| `.#checks.x86_64-linux.fork-instances-restart` | about 85 s |
| `.#checks.x86_64-linux.fork-overlay-store` | about 90 s |

The fork's own tests are imported into this flake's `checks` as `fork-*`, so CI for this repo covers them and they evaluate quickly; building them through the fork's own `checks` evaluates its whole hypervisor matrix first. Add `--override-input microvm git+file://...` to run any check against an unpushed fork checkout.

Run only the tests that cover a change. Several installables in one `nix build` run in parallel, up to the builder's `max-jobs`; when one fails, the others in the same invocation are cut off, so rerun them before trusting them.

To see why a test failed:

```bash
nix build ... > test.log 2>&1
grep -a -E "RequestedAssertionFailed|AssertionError|Traceback|error\[" test.log
```

Lines from guests appear as `microvm@machine-<name>[pid]:` in the host's output.

## Rules for tests

- The test network has no internet. Nothing in a test may need it: no substitution, no flake registry fetches, no Tailscale login, no running flox. `checks/lean-guest.nix` disables substituters. List anything that needs the network in `docs/testing.md` under "Not covered by tests".
- Every command run in a guest goes through `timeout`, e.g. `timeout 60 machine ssh alice …`, and every wait has a `timeout=`. A guest hang then fails in a minute instead of at the test timeout.
- Run commands as the owner with `runuser -u alice -- …`, not `su -`. `su -` sessions hung in nested guests.
- Give the test VM 4 cores when it runs two or more guests and 2 cores for one guest, with `-cpu host`.
- Keep tests to what proves the feature works and data persists.

## Nix string escaping

The CLI and test scripts are shell and Python inside Nix strings.

- In `''…''` strings, write shell `${VAR}` as `''${VAR}`.
- In `"…"` strings, write it as `\${VAR}`.
- A Python f-string inside a `''…''` test script writes literal braces as `{{` and `}}`.

## Conventions

- Work in a branch and a worktree under `.worktrees/`.
- Conventional commit messages, one logical change per commit.
- Run `nix fmt` before committing; the `formatting` check fails on unformatted files.
- Record design choices not covered by a spec in `docs/decisions.md`.
- Specs go in `docs/superpowers/specs/`, implementation plans in `docs/superpowers/plans/`.
