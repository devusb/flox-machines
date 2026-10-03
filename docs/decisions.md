# Decisions

Decisions made during design that are not themselves part of the design spec, with the reasoning at the time.

## 2026-10-03 microVMs, not containers

Each person's machine is a microVM with its own kernel. systemd-nspawn containers were considered because NixOS containers support imperative creation from one flake output, share the host nix-daemon, map GC roots per container, and update in place. They were rejected because the agent executing tool calls is untrusted code. The systemd-nspawn documentation states that user namespacing off "is not secure and must not be used to run untrusted code," that the per-user option "is not a security feature," and the strongest mode still shares the host kernel. The imperative NixOS container CLI also does not write the private-users setting. Commercial agent sandboxes run Firecracker microVMs for the same reason.

## 2026-10-03 Build on microvm.nix, with a fork

microvm.nix provides declarative VM definitions, systemd integration, virtiofs and vsock handling, and a CLI. Running cloud-hypervisor directly under hand-written systemd units was considered and rejected as re-implementing that. microvm.nix lacks instances from a template and bakes per-VM parameters into the runner, so those are added in a fork under the flox org and proposed upstream.

## 2026-10-03 Overlay store over the host store

Each guest's store is a `local-overlay` store whose lower layer is the host's `/nix/store` and `/nix/var`, shared read-only, with a persistent upper layer and a persistent `/nix/var` on per-instance volumes. This is forest.nix's layout with the upper layer kept across restarts. Chosen by Morgan over a guest-owned store seeded from the host at boot, which would be consistent by construction but copies the base closure into every instance and does not share the host's paths. The cost is that host store paths a guest references can disappear when the host collects garbage; the boot-time `nix-store --verify --repair` restores substitutable ones. The model will be revisited once it runs.

## 2026-10-03 Host garbage collection is manual, with instances stopped

The host store mostly grows. Collecting garbage on the host is a manual operation run with every instance stopped, since overlayfs requires the lower layer not to lose paths while mounted. No GC roots are kept on the host for paths guests reference. Decided by Morgan.

## 2026-10-03 Guest state on its own volume

The guest's `/nix/var`, holding the overlay store's database, profiles and GC roots, is a volume separate from the upper store layer. The upper layer and the database must survive together for the store to stay consistent, and NixOS clients write profiles under `/nix/var`, which is on the root tmpfs otherwise.

## 2026-10-03 Every guest client uses the daemon

`NIX_REMOTE=daemon` is set for all guest sessions and services, and only the daemon opens the `local-overlay` store. A root client opening the merged `/nix/store` as a plain local store would write whiteouts over host paths when collecting garbage.

## 2026-10-03 Machine sizes come from the template

Memory and vCPUs default to the template's values, so changing them in the host configuration resizes every instance without an override at the restart the change triggers. `machine create` writes no size into `instance.env`. `machine resize` writes a per-instance override and `machine resize --reset` removes it.

## 2026-10-03 Host rebuild restarts instances

Base updates are applied by rebuilding the host, which restarts every instance whose base changed. Live in-place updates by copying the closure into each guest and running switch-to-configuration were considered and rejected because they add a deploy step and leave guests on old kernels until a later reboot. The cost is that running sessions die on base changes, which is accepted because agent history is recovered with resume.

## 2026-10-03 Guests are user-owned Tailscale nodes

Each guest is claimed by its owner through a login link, so the only policy rule needed is the self autogroup. The alternative of host-minted tagged auth keys with one tag per person was rejected because it requires a service with write access to the whole tailnet policy and makes admin access rules per person. Admin access to guests is from the host over vsock SSH instead of over the tailnet.

## 2026-10-03 Home is a block volume, not a share

Home is a ZFS zvol attached as a block device. A virtiofs share from a host dataset was considered because it would let the host see the person's files, and rejected because git on large repositories wants block performance and nothing on the host needs to read home.

## 2026-10-03 Credentials are self-service

People log into GitHub, model providers and other services from inside the guest and the tokens live in home. Vault or 1Password injection was considered and rejected for v1 as friction. Anything in home is readable by any agent the person runs, and this is stated to users.

## 2026-10-03 Both home-manager and Flox in the base

Flox is the software layer and home-manager is the configuration layer. Both are in the guest base with the host flake's inputs pinned in the guest registry so they resolve consistently. home-manager is optional and ships with a starter flake template.

## 2026-10-03 cloud-hypervisor

Chosen among microvm.nix's hypervisors because its runner supports vsock with systemd notify and vsock SSH, virtiofs, and a control socket for clean shutdown. qemu is the fallback if a device need arises that cloud-hypervisor lacks.

## 2026-10-03 Hetzner dedicated, single host

One bare-metal host. Cloud instances with nested virtualization were considered and rejected for overhead and availability. Multi-host placement is out of scope until the single host is full.

## 2026-10-03 No Coder or hosted alternative

Coder offers workspaces, a web terminal, idle autostop and an agent task UI. Building was chosen because the organization is Flox and the point is a Nix and Flox native appliance with Tailscale identity, and because the shape is small: one host flake, one template, a CLI and a front door.

## 2026-10-03 Mobile is Tailscale SSH

A tool-agnostic structured mobile UI would need a client speaking a protocol such as the Agent Client Protocol to agents on the box. Deferred until the box itself is boring. Tailscale on the phone with Blink or Termius and mosh needs nothing from the platform.

## 2026-10-03 Direct Tailscale connections through a forwarded UDP port

Guests sit behind the host's NAT. Rather than relying on NAT traversal through the host's masquerade, each instance is allocated a UDP port on the host's public IP that is forwarded to the guest's tailscaled, so peers connect directly and DERP relays are only a fallback. Giving guests public IPv4 addresses was rejected as wasteful; a routed IPv6 prefix, when present, is used in addition.

## 2026-10-03 No starter Flox environment

Flox is in the guest system closure and nothing else. A starter default environment built from an agent stack was considered and dropped so that environments are entirely the person's and the base has no opinion on tools.

## 2026-10-03 Person files are opaque to the host

Home and state volumes are block devices attached only to their own guest, never mounted on the host. A host admin has no path to cd into anyone's home. Encrypting the volumes inside the guest was considered: with the key stored on the host it only obfuscates against host root, and with a passphrase the person must re-enter it after every restart. Deferred as optional. Host root is accepted as able to read everything with effort.

## 2026-10-03 Develop against NixOS tests

The host module and the fork's instances feature are built test-first with the NixOS test framework, following microvm.nix's own checks that run guests under nested KVM. This covers instance creation, restart on base change and both storage backends. Tailscale cannot be exercised in a test network, so the claim flow is verified by hand.

## 2026-10-03 Image and zvol storage backends

Instance volumes are image files or zvols behind one option. Image files are what microvm.nix uses natively and what tests and workstations run; zvols are for the production host. Spike 1 ran on image files.
