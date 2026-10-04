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

## 2026-10-03 Guests are user-owned Tailscale nodes

Each guest is claimed by its owner through a login link, so the only policy rule needed is the self autogroup. The alternative of host-minted tagged auth keys with one tag per person was rejected because it requires a service with write access to the whole tailnet policy and makes admin access rules per person. Admin access to guests is from the host over vsock SSH instead of over the tailnet.

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

Persistent volumes are block devices attached only to their own guest, never mounted on the host. A host admin has no path to cd into anyone's home. Encrypting the volumes inside the guest was considered: with the key stored on the host it only obfuscates against host root, and with a passphrase the person must re-enter it after every restart. Deferred as optional. Host root is accepted as able to read everything with effort.

## 2026-10-03 Develop against NixOS tests

The host module and the fork's instances feature are built test-first with the NixOS test framework, following microvm.nix's own checks that run guests under nested KVM. This covers instance creation, restart on base change and both storage backends. Tailscale cannot be exercised in a test network, so the claim flow is verified by hand.

## 2026-10-03 Image and zvol storage backends

Each machine's persistent volume is an image file or a zvol behind one option. Image files are what microvm.nix uses natively and what tests and workstations run; zvols are for the production host.

## 2026-10-03 Admin access to machines is SSH over the host bridge

The host generates one SSH key at `/var/lib/flox-machines/id_ed25519`. Each machine's instance directory carries its public key, and the machine installs it for root at boot. Admins reach machines with `machine ssh` over the host-only bridge. vsock SSH was considered and set aside because cloud-hypervisor exposes vsock through a per-VM socket multiplexer that needs extra client configuration.

## 2026-10-03 Bridge addressing from dnsmasq

The host bridge has a /24, and dnsmasq hands out addresses with the machine's hostname recorded in the leases file. `machine ssh` looks machines up there.

## 2026-10-03 Base version marker

Every machine has `/etc/machine/base-version`, set from `floxMachines.baseVersion`, so a person and the tests can see which base a machine booted.

## 2026-10-03 flox from its own flake

Both the host and the machine template import `nixosModules.flox` from `github:flox/flox/latest`. The module installs the Flox CLI and adds cache.flox.dev and its signing key to `nix.settings`. The flake is not made to follow the platform's nixpkgs, so its packages come from cache.flox.dev.

## 2026-10-03 Admin SSH resolves machines by MAC

`machine ssh` finds a machine's address from the DHCP lease whose MAC matches the instance's `MICROVM_MAC_0`, not by hostname, because the guest chooses the hostname it sends. Guest SSH host keys are not pinned yet.

## 2026-10-03 Daemon-only store access is set by environment

Guest clients reach the overlay store through `NIX_REMOTE=daemon` in the session and service environment. Setting `store = daemon` in `nix.conf` would also apply to the guest's nix-daemon and point it at itself.

## 2026-10-03 User services start after the store repair

Each boot, a separate `machine-linger` service enables linger for the owner after `microvm-verify-store` finishes, so the owner's user units start only once missing store paths have been restored. Account creation stays in its own service before SSH, so logging in does not wait for repair downloads. Linger is not persisted, because `/var/lib/systemd` is on the guest tmpfs.

## 2026-10-03 Owners have passwordless sudo

The owner's account is in `wheel`, and `wheel` needs no password for sudo. Decided by Morgan. Owners have root inside their own machine; the hypervisor remains the boundary between machines and the host. The instance share is mounted read-only so root in a guest cannot change files the host reads back.

## 2026-10-03 Tailscale in the template

The template enables `services.tailscale`. tailscaled's state directory `/var/lib/tailscale` is kept on the persistent volume, so the node identity and `tailscale serve` configuration survive restarts and base updates. Tailscale SSH is turned on through `services.tailscale.extraSetFlags`. No operator is set, so the owner runs `tailscale serve` with sudo. Joining the tailnet is the claim flow and is not automated yet.

## 2026-10-04 One persistent volume with impermanence

Each machine has a single persistent volume at `/persist`, and the impermanence module binds the paths worth keeping from it: `/home`, `/var/log`, `/var/lib/nixos`, `/var/lib/systemd/coredump`, `/var/lib/systemd/timers` and `/var/lib/tailscale`, following impermanence's recommended list for a headless system. The SSH host key is kept at `/persist/etc/ssh` through `services.openssh.hostKeys`. With ZFS this is one zvol per machine, so one snapshot captures everything that matters about a machine. The Nix store layers stay separate image files, because they are rebuildable and would fill snapshots with churn. Separate home and state zvols, more zvols for the store layers, and a dataset per machine were considered. The persistent volume is marked `neededForBoot`, as impermanence requires.

## 2026-10-04 Machines reach the host only for DHCP

The host does not trust the bridge. Only DHCP is allowed from machines, through `networking.firewall.interfaces.<bridge>.allowedUDPPorts`, plus ping and replies to connections the host opened. dnsmasq does not answer DNS on the bridge; machines get public resolvers over DHCP. Ports opened globally, including by `services.openssh.openFirewall`, still reach machines, so the module warns about them and the README says to open host services per interface. The module does not change the SSH setup itself, so it cannot lock an admin out. A custom nftables table that dropped everything from the bridge was considered and rejected in favor of the standard options.

## 2026-10-04 Machines are isolated from each other on the bridge

Machine taps are isolated bridge ports through networkd's `Isolated=` setting, so machines cannot reach each other over the bridge. They can still reach each other over the tailnet.

## 2026-10-04 No MAC or IP pinning on bridge ports

Pinning each tap to its MAC and IP needs hand-written bridge filtering rules; NixOS and networkd have no option for it. With isolation, the remaining risk is a machine spoofing another machine's MAC to take its DHCP lease, so that `machine ssh` reaches the wrong machine. The planned fix is admin access over vsock, which microvm.nix supports for cloud-hypervisor through each VM's own socket, so `machine ssh` stops depending on the network.

## 2026-10-04 The front door runs as its own user

The front door runs as `flox-machines-front-door` with its state in `/var/lib/flox-machines-front-door`. Sudo rules let it run only `machine create`, `machine status` and `machine login` as root. Putting its state under the root-only `/var/lib/flox-machines` with a shared group was considered and rejected, because that directory holds the admin SSH key. `oauthSecretFile` must not be a Nix store path.

## 2026-10-04 virtiofsd stays root

microvm.nix runs virtiofsd as root. Running it unprivileged needs user-namespace uid mapping so guests still see root-owned store files, and the read-only host Nix database must stay readable to it. That is a change in the microvm.nix fork with real breakage risk, deferred until the rest is stable.

## 2026-10-04 Machines keep their base until restarted

A host rebuild that changes the template updates every machine's runner but restarts none of them by default. Running machines keep their booted base until `machine restart`, `machine resize` or a reboot from inside the machine, and interactive shells show a notice when a newer base is waiting. The host writes the template's current system path into each machine's read-only instance share as `instance/system` for that comparison. `floxMachines.restartOnUpdate = true` restarts machines on every template change instead. Chosen by Morgan so running sessions are never interrupted by an update; the cost is that fixes in the base wait for each person's restart.

## 2026-10-04 A reboot inside a machine powers it off

cloud-hypervisor handles a guest reboot itself and keeps its configuration, so a machine that rebooted itself came back on the base it booted with. The template makes systemd's reboot end as a power-off; cloud-hypervisor exits, and the host's `microvm@` unit, which always restarts, starts the machine again from the current runner. `sudo reboot` inside a machine therefore takes a waiting base.

## 2026-10-04 No waiting on timeouts

`machine destroy` kills the VM before stopping its unit, because a machine still in its initrd ignores the shutdown request and its data is being deleted anyway. The host's network-online wait ignores the machine bridge, which has no carrier until a machine starts, and the front door does not wait for network-online because tsnet retries on its own. Tests assert deadlines for these paths so a reintroduced timeout fails them.

## 2026-10-04 Machines collect garbage twice a month

The template enables `nix.gc` on the 1st and 15th of each month at 03:00, with up to six hours of random delay and catch-up after downtime. It removes unreferenced paths only; old profile and home-manager generations are kept so people can roll back. The machine's store is a `local-overlay` store, so collection only removes paths from the machine's upper layer, never from the host's store.

## 2026-10-04 one vsock CID for every machine

Every machine gets `microvm.vsock.cid = 3`, which lets cloud-hypervisor pass systemd's readiness notification to the host. Cloud-hypervisor backs vsock with a per-VM unix socket instead of the host's vhost-vsock device, so CIDs do not have to be unique across machines on one host.
