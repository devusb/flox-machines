# Flox Machines: design

A NixOS host that turns a tailnet identity into a personal NixOS microVM built from one generic configuration. The person gets a persistent box only they can reach, with Flox for software and home-manager for configuration, and nothing to administer. The admin manages one flake.

## Goals

- A person in the tailnet gets a persistent remote machine with one tap and no setup.
- The machine is reachable only by its owner over Tailscale, from laptop or phone, with no keys to manage.
- The person installs anything with Flox, optionally manages configuration with home-manager, keeps credentials on the box, runs services and containers, and attaches to long-running agent sessions from any device.
- The person never has to run an OS update or see Nix unless they choose to. They have root inside their own machine.
- The admin updates every machine by rebuilding the host.
- Each machine is a kernel boundary. A compromised agent is confined to its owner's machine, credentials and network grants.

## Non-goals for v1

- Multiple hosts, placement, or migration.
- A structured mobile chat UI. The phone path is Tailscale SSH and tmux.
- Egress filtering.
- Dynamic memory management. Memory is allocated per guest.
- Centralized credential injection. Credentials are self-service inside the guest.

## Overview

```
                 tailnet
    laptop ─────┐    ┌──── phone
                │    │
        ┌───────▼────▼──────────────────────────────┐
        │ host (NixOS, Hetzner dedicated, ZFS)       │
        │                                            │
        │  front door  ──creates──►  instance CLI    │
        │  (tailscale serve)                         │
        │                                            │
        │  template "machine"  ──runner──►  current  │
        │                                     │      │
        │   ┌───────────┐  ┌───────────┐      ▼      │
        │   │ vm: alice │  │ vm: bob   │  microvm@*  │
        │   │ tailscaled│  │ tailscaled│             │
        │   │ home zvol │  │ home zvol │             │
        │   └───────────┘  └───────────┘             │
        └────────────────────────────────────────────┘
```

The host declares the base once, as a template. Instances of the template are created at runtime, one per person, and are never declared. A host rebuild regenerates the template and restarts every instance on it. Instances cannot customize the base: there is no per-instance NixOS configuration, only resources in `instance.env` and whatever the person does inside with Flox and home-manager. Changing the base is an admin change to the template, applied to everyone.

## Packaging

The platform is a NixOS module, `floxMachines`, that an admin enables on any NixOS host. Enabling it imports the forked microvm.nix host module, registers the `machine` template, installs the instance CLI, runs the front door, and adds the snapshot job. It coexists with VMs the host declares the ordinary way in `microvm.vms`: instances live under the same microvm state directory with a `machine-` prefix and use the same bridge, systemd template units and CLI. The module exposes a small set of options: the template to use, the bridge name, the default memory and vcpus for new instances, the storage backend for instance volumes, the front door hostname, and the admin group.

Storage backends are `image`, where each volume is a file under the instance directory, and `zfs`, where each volume is a zvol under a parent dataset and the instance directory holds a link to the device. Both present the same block device to the guest. Tests and workstations use `image`; the production host uses `zfs`.

## Host

- NixOS on a Hetzner dedicated server. Root on ZFS. One ZFS zvol per instance for home and one for guest state.
- The `floxMachines` module, which brings in the microvm.nix host module from the Flox fork (see "microvm.nix fork") with cloud-hypervisor as the hypervisor. Other microvm.nix VMs on the host are unaffected.
- A bridge with NAT for guest egress. Each instance gets one UDP port on the host's public IP forwarded to its guest's tailscaled, so Tailscale peers connect to the guest directly rather than through DERP relays. If the host has a routed IPv6 prefix, guests also get a global IPv6 address on the bridge for direct connections without NAT. No other inbound ports. All access to a guest is over that guest's own Tailscale node.
- The host is a tagged Tailscale node reachable by admins only. Admins reach any guest from the host over vsock SSH. Members of the admin group see every instance in the front door.
- The front door service, behind Tailscale serve.
- The instance CLI, a wrapper over the forked `microvm` command, used by the front door and by admins.
- Scheduled ZFS snapshots of each machine's persistent zvol, replicated to a Hetzner storage box.
- Host rebuilds are applied by an admin or CI in a known window. The host does not auto-upgrade.

## Template: the guest base

One NixOS configuration, `machine`, exported from the flake and registered as a microvm template on the host. Everything per person comes from the instance directory at boot. The template contains:

- **Identity.** The `microvm.instance` guest module mounts the instance directory, sets the hostname, and loads systemd credentials from it. The person's username and SSH keys come from the instance file.
- **Access.** tailscaled with Tailscale SSH. No sshd on the network. The guest's Tailscale state lives on the persistent volume so the node identity survives reboots.
- **Persistence.** The root is tmpfs. One persistent volume is mounted at `/persist`, and the impermanence module binds `/home` and `/var/lib/tailscale` from it. The SSH host key is kept at `/persist/etc/ssh`. Everything else on the root is rebuilt each boot.
- **Account.** One user named after the person, no password, with passwordless sudo through `wheel`. Root is also reachable from the host over the bridge with the admin key.
- **Sessions.** tmux and agent-deck. The login shell attaches to the person's session. mosh for the phone.
- **Software.** flox is in the system closure. Environments belong to the person. The base creates none and the shell integration is flox's own.
- **Configuration.** home-manager available as a standalone tool. A flake template in the platform repo gives a working home configuration with shell, tmux, agent-deck and Flox activation wired. Using it is optional.
- **Registry pin.** The host flake's `nixpkgs` and `home-manager` inputs are set as the guest's flake registry entries and nix path, so every guest and every person's home-manager flake resolve to the inputs the base was built from.
- **Containers.** Rootless podman with the docker compatibility shim.
- **Services.** Any port the person opens is reachable at the guest's tailnet hostname. Tailscale serve is available to the person for HTTPS.
- **Store.** A `local-overlay` store over the host store with persistent upper layer and state. See "Store".
- **Status.** The login shell prints when the base last changed.

## Instances

### Layout

```
/var/lib/microvms/machine-<name>/
  template          name of the template this instance was created from
  current           symlink to the template's runner, refreshed on host rebuild
  booted            symlink to the runner that booted the running guest
  instance.env      hostname, memory, vcpus, tap id, mac, vsock cid, tailscale udp port
  instance/         shared into the guest read-only
    user            username
    keys            ssh public keys
    credentials/    files loaded as systemd credentials
  persist.img       the persistent volume, or a symlink to its zvol
```

### Create

1. The person opens the front door, which identifies them from Tailscale headers. They tap create.
2. The front door runs `machine create <localpart>`.
3. The CLI creates the persistent volume, writes the instance directory and `instance.env`, links `current` to the template runner, and starts `microvm@<name>`.
4. The guest boots, creates the user, and starts tailscaled with no auth key. The guest does not join the tailnet on its own. tailscaled generates its login URL, the guest writes it to the state volume, and the front door shows it. The person taps it and logs in to Tailscale in their own browser. The front door never sees or handles their credentials. The node is now owned by the person.
5. The front door polls the guest until it is authenticated, then disables key expiry for that device through the admin API.
6. The front door shows the SSH command and the phone instructions. If the node ever needs re-authentication, the same URL path reappears.

### Update

A host rebuild regenerates the template runner and refreshes `current` for every instance. Each instance service has the guest closure as a restart trigger, so instances whose base changed are shut down cleanly through the hypervisor control socket and started on the new base. Instances whose base did not change are untouched. Running sessions and agents on a restarted guest are gone. Agent history is recovered with each tool's resume.

### Restart, re-image, resize, backup, offboard

- **Restart.** `machine restart <name>` or a systemd restart of the instance service.
- **Re-image.** Stop, delete the upper store and `/nix/var` volumes, start. Home and state remain. Home-manager generations, `nix profile` installs and anything else in the guest store are removed, and user units that home-manager linked into home stay broken until the person runs `home-manager switch` again.
- **Resize.** The template sets the default memory and vCPUs. Changing them in the host configuration resizes every instance that has no override, at the restart the change triggers. `machine resize` writes an override into `instance.env` for one instance, and `machine resize --reset` removes it.
- **Backup.** ZFS snapshots of the persistent zvol. Restore is a rollback or clone with the machine stopped.
- **Offboard.** `machine destroy <name>`: stop, remove the instance directory, snapshot and schedule the zvols for deletion after a retention period, delete the device from the tailnet through the API.

## Identity and network

- Each guest is a Tailscale node owned by the person who claimed it. The policy needs one grant: members may reach their own devices, which is the self autogroup. Nothing in the policy is per person.
- Tailscale SSH on the guest accepts the owner as the guest's non-root user. Key expiry is disabled for guest nodes after claim, through the API.
- Admin access to guests is from the host over vsock SSH as root, not over the tailnet.
- The guest's tailnet reach is whatever its owner's identity is granted. For sensitive destinations, Tailscale SSH check mode forces a browser re-authentication within a configured period.
- The host has a bridge with NAT. Guests get a tap interface each, with the id and MAC taken from `instance.env`.
- Each guest's tailscaled listens on the UDP port from `instance.env`, and the host forwards that port from its public IP to the guest. Peers reach the guest directly; DERP is the fallback only. The instance CLI allocates the port on create and opens it in the host firewall.

## Store

Each guest's Nix store is a `local-overlay` store, using the layout forest.nix uses for its guests.

- **Lower layer.** The host's `/nix/store` at `/nix/.ro-store` and the host's `/nix/var` at `/nix/.ro-var`, both shared read-only over virtiofs. Every path valid on the host is valid in the guest with no copy.
- **Upper layer.** A per-instance volume at `/nix/.rw-store`. Paths the guest builds or fetches, including Flox environments and home-manager generations, land here.
- **Guest state.** A second per-instance volume at `/nix/var` holds the guest's database, profiles and GC roots.
- **Daemon.** The guest's nix-daemon opens the store as `local-overlay://` with the host database as the lower store. Every client, including root, goes through the daemon.

Both volumes persist across restarts and base updates. A restart re-pulls nothing.

At boot, before user sessions, the guest runs `nix-store --verify --repair`. It drops database entries for missing paths that nothing refers to, and substitutes missing paths that the guest's paths still reference.

The host store mostly grows. Host garbage collection is a manual operation with every instance stopped, because the overlay's lower layer must not lose paths while mounted. Paths the host collects that a guest still references are restored by that guest's repair at its next boot. Locally built paths with no substitute cannot be restored.

## Security model

- The boundary between people and between a person and the host is the hypervisor. Each guest has its own kernel.
- Inside a guest, the agent runs as the person. Anything in the person's home, including credentials, is readable by any agent they run there. That is the written contract.
- A compromised agent can reach what the guest's owner can reach on the tailnet, plus the internet. Check mode on sensitive destinations is the control.
- Guests cannot reach the host's nix-daemon or any host service except the hypervisor's virtio devices and vsock SSH, which only the host initiates. Guests can read the host's store and `/nix/var`, including the host's profiles and GC roots, read-only.
- The persistent volume is attached only to its own guest. The host never mounts them and has no filesystem path into a person's files. Between guests, the hypervisor is the boundary.
- Admins are root on the host and could mount any zvol. This is stated to users. LUKS inside the guest with the key on the state volume is an optional later addition that raises the effort for a host admin without changing that line.

## Tier-0 assets

Anyone holding one of these can read or change every machine, so each is handled as tier 0.

| Asset | What it gives | Handling |
|---|---|---|
| Root on the host | Every machine's disks, memory and `/persist`, the admin SSH key, the front door's identity | Few admins. SSH only on the public interface, key-only, through Tailscale where possible. No other services on the host |
| The Hetzner account | Rescue boot, reinstall and console access, which bypass host root entirely | Hardware-key 2FA on every login. No shared logins. API tokens scoped to the server and kept off the host |
| Backups on the storage box | Every person's home and credentials, as of each snapshot | Encrypted on the host before sending, with the key not stored on the storage box. The storage box reachable only with a key dedicated to the host, and append-only where the protocol allows |
| The Tailscale admin console and the front door's OAuth client | Changing the policy that keeps machines apart; minting `tag:flox-machines` keys | Hardware-key 2FA for admins. The OAuth client scoped to creating auth keys for `tag:flox-machines` only, stored in a root-only file outside the Nix store, rotated when an admin leaves |
| The flake repository and its deploy path | Arbitrary code as root on the next host rebuild | Protected branch, reviewed merges, deploys only from that branch |

Encryption of the machine zvols at rest, unlocked at boot over SSH in the initrd, is an option for a later version. Without it, physical access to the disks or the Hetzner rescue system reads them.

## Front door

A small HTTP service behind Tailscale serve at a fixed tailnet hostname. Tailscale serve adds identity headers, so the service has no login of its own. It shows the viewer's instance if one exists, with its state, the claim link if unclaimed, the SSH command, and buttons for restart and re-image. If no instance exists it shows create. It shells out to the instance CLI. Admins see all instances.

## Phone

Tailscale on the phone, Blink or Termius with mosh, Tailscale SSH to the guest, which lands in the person's tmux session. This needs nothing beyond the base. Optional later: a web terminal behind the guest's Tailscale serve, and an ntfy instance so agent hooks can push "needs input" notifications.

## microvm.nix fork

Maintained under the flox GitHub org and consumed as the host flake's microvm input. Each feature is proposed upstream as an issue first and kept as its own series.

### Feature 1: instances

- **Host module.** `microvm.templates.<name>` accepts a config or flake output and builds one runner. The install unit refreshes `current` for every instance directory whose `template` file names it. Instance services carry the guest closure as a restart trigger, with a template-level `restartIfChanged` default of true.
- **CLI.** `microvm -c <name> -t <template>` creates an instance directory and `instance.env`, with volumes auto-created from the template's relative image paths. `microvm -l` lists instances with their template and staleness. `microvm -R` restarts stale instances.
- **Runner late binding.** The runner script reads `instance.env` from its working directory for hostname, memory, vcpus, interface id, MAC and vsock CID, with the values from the configuration as defaults. Today these are baked into the script at build time. The Tailscale port is not a runner concern; the guest reads it from the instance share and the host firewall rule comes from the instance CLI.
- **Guest module.** `microvm.instance` declares the instance share at a fixed mount point, sets the hostname at boot, and loads systemd credentials from the instance directory.

### Feature 2: overlay store

- Guest option `microvm.overlayStore` sets up the store described in "Store": the host `/nix/var` share, the upper and `/nix/var` volumes, the daemon's `local-overlay` store URL, `NIX_REMOTE=daemon` for every client, and the boot-time verify and repair unit.

## Testing

The module and the fork's instances feature are developed against NixOS tests. The test node is the host, with nested KVM. The test script creates instances with the CLI, checks the guests over vsock SSH, switches the host to a specialisation with a changed template, and asserts that only instances on that template restarted and that their volumes kept their contents. A second test exercises the `zfs` backend with a pool on an extra disk. Guests in tests use a stub in place of tailscaled, since the test network has no tailnet. The claim flow is verified by hand, as in spike 1.

## Spikes, in order

1. **Claim flow.** tailscaled in a cloud-hypervisor guest with state on a volume, login URL surfaced to the host, Tailscale SSH landing in tmux as the right user, and a direct connection from a laptop confirmed with the forwarded UDP port rather than a relay.
2. **Instance restart on host rebuild.** A template with two instances, a base change, confirm only those two restart and come up on the new base with home intact.
3. **Rootless podman in the guest** with the docker shim, running a typical compose file.
4. **Flox and home-manager in the guest** against the guest daemon, re-materialization after a reboot, and how long it takes for a typical environment.
5. **Runner late binding** for interface id and MAC, confirming two instances of one template coexist on the bridge.

## Open questions

- Memory per instance by default, and the host size that implies for expected concurrent use.
- The retention period for offboarded machines' zvols.
