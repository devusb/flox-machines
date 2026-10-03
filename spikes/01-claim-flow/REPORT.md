# Spike 1: claim flow

Throwaway. The flake here is not part of the platform.

## Question

Can a microvm.nix guest join the tailnet by surfacing a login URL for its owner to tap, with no credentials handled by the host, and does the node identity survive a reboot?

## Setup

- Guest built from `flake.nix` with the upstream microvm.nix guest module, run with the qemu runner and user networking on a workstation. No root, no bridge.
- Persistent volumes: `tailscale-state.img` at `/var/lib/tailscale`, `home.img` at `/home`.
- tailscaled started with no auth key. A service polls `tailscale status` and prints the login URL to the console.
- One user, `mhelton`, matching the tailnet login's local part. Bash init execs tmux on SSH logins.

## Results

| Check | Result |
|---|---|
| Guest boots to login URL | 14 s on the q35 machine type |
| Owner taps URL | Node appears as `claimspike`, owned by the tapping user |
| Tailscale SSH from another node of the same user | Works with no key setup. Default policy allowed it without a check prompt |
| Interactive login | Lands in tmux session `main` as `mhelton` |
| Reboot | Rejoins in 14 s with no new login URL. SSH works after |
| Connection path | Relayed through DERP on first boot, direct via the host's LAN address and a NAT-mapped port after the reboot. Direct connection through a forwarded port on a public IP is still the Hetzner spike |

## Findings to carry into the base

- The home directory was not created. NixOS activation made it on the root filesystem before the home volume was mounted, and the mount hid it. The base must mount persistent volumes before activation or create the home directory after the mount, for example with `neededForBoot` on the home filesystem or a tmpfiles rule.
- The qemu `microvm` machine type hangs the 6.18 kernel on a Ryzen 7640U before SMP bringup. q35 boots. Production uses cloud-hypervisor, so this only affects workstation spikes.
- `tailscale up --ssh` with `--reset` is idempotent across reboots: once state exists it returns immediately.
- The login URL is available from `tailscale status --json` as `AuthURL`, which is what the front door would read from the guest.

## Not covered

- Direct connections through a forwarded UDP port.
- Disabling key expiry through the API after the claim.
