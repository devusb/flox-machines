# Flox Machines

Flox Machines gives each person a persistent NixOS microVM built from one template the admin declares. Machines are created imperatively, need no administration from the person, and restart onto a new base when the host is rebuilt. Inside, people install software with Flox and manage configuration with home-manager.

## Enabling it on a host

```nix
{
  inputs.flox-machines.url = "github:devusb/flox-machines";

  outputs = { nixpkgs, flox-machines, ... }: {
    nixosConfigurations.host = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [
        flox-machines.nixosModules.floxMachines
        {
          floxMachines = {
            enable = true;
            bridge.externalInterface = "enp1s0";
          };
        }
      ];
    };
  };
}
```

| Option | Default | Meaning |
|---|---|---|
| `floxMachines.template` | the bundled template | guest NixOS module every machine runs |
| `floxMachines.defaults.mem` | `4096` | memory in MB for machines without an override |
| `floxMachines.defaults.vcpu` | `2` | vCPUs for machines without an override |
| `floxMachines.defaults.persistSize` | `102400` | persistent volume in MB, holding `/home` and machine state |
| `floxMachines.defaults.storeSize` | `204800` | Nix store upper layer in MB |
| `floxMachines.defaults.nixVarSize` | `51200` | Nix state in MB: the machine's Nix database, profiles and builds in progress |
| `floxMachines.storage` | `"image"` | `"image"` or `"zfs"` |
| `floxMachines.zfs.parentDataset` | `null` | dataset for machine zvols when storage is `zfs` |
| `floxMachines.bridge.externalInterface` | `null` | interface machines are NATed through |

## Commands

| Command | Effect |
|---|---|
| `machine create <name>` | create and start `machine-<name>` with user `<name>` |
| `machine status <name> [--json]` | show a machine's owner, whether it runs, and its Tailscale state and login link; `--json` prints it as JSON |
| `machine ssh <name> [command]` | run a command as root on the machine |
| `machine restart <name>` | restart the machine |
| `machine resize <name> <mem-MB> <vcpu>` | set a per-machine size and restart |
| `machine resize <name> --reset` | return to the template size and restart |
| `machine grow <name> persist\|store\|var <MB>` | grow a machine's persistent volume, Nix store layer or Nix state volume and restart it; volumes cannot shrink |
| `machine reimage <name>` | wipe the machine's Nix store layer and Nix state, then restart; home is kept, but home-manager generations and `nix profile` installs are removed, so the person runs `home-manager switch` again |
| `machine destroy <name>` | stop the machine and delete it with its volumes |
| `machine list` | list machines and whether they run the current base |
| `machine gc` | stop all machines, collect garbage on the host, start them again |

A host rebuild that changes the template restarts every machine onto the new base. A rebuild that does not change it restarts nothing.

## Store

Each machine's Nix store layers its own persistent upper layer over the host's store, which it reads but never writes. Paths a person installs survive restarts and base updates. Host garbage collection runs only through `machine gc`; enabling `nix.gc.automatic` or `nix.settings.min-free` on the host is an evaluation error. Each machine repairs missing referenced paths from its substituters at boot.

## Storage backends

Each machine has one persistent volume mounted at `/persist`. The guest root is tmpfs. These paths live on `/persist` through the impermanence module: `/home`, `/var/log`, `/var/lib/nixos`, `/var/lib/systemd/coredump`, `/var/lib/systemd/timers` and `/var/lib/tailscale`. The SSH host key is kept in `/persist/etc/ssh`. The Nix store layers are separate image files that `machine reimage` deletes.

Anything written elsewhere, including system changes made with sudo, resets when the machine restarts. Keep what should last in home, in Nix or Flox environments, or add it to the template's persistence list.

The owner's login shell is kept across restarts. bash, fish and zsh are in the base; change it with `sudo chsh -s /run/current-system/sw/bin/fish <name>`. The shell is saved to `/persist/etc/machine/shell` and restored at boot, falling back to bash if the saved path is not executable.

Each machine collects garbage in its own Nix store on the 1st and 15th of the month. Only unreferenced paths in the machine's layer are removed; old generations stay, and the host's store is never touched.

With `storage = "image"`, the persistent volume is `persist.img` under `/var/lib/microvms/machine-<name>/`. With `storage = "zfs"`, it is the zvol `<parentDataset>/<name>`, auto-snapshotted. Snapshot a machine with `zfs snapshot <parentDataset>/<name>@<label>`; roll back with the machine stopped.

Volume sizes are ceilings. Image files and zvols are sparse, so the host only uses space a machine has written, and that space is not returned when the machine deletes files. `persistSize`, `storeSize` and `nixVarSize` apply when a volume is created; changing them leaves existing machines alone. `machine grow` enlarges one machine's volume, and `machine reimage` recreates the store layer and Nix state at the current `storeSize` and `nixVarSize`.

## Tests

Tests are NixOS tests that run nested VMs. Build them on a machine with KVM:

```bash
nix build -L .#checks.x86_64-linux.create-restart
nix build -L .#checks.x86_64-linux.store-reboot
```

## Front door

The front door is a web page on your tailnet where people create and claim their own machine.

```nix
floxMachines.frontDoor = {
  enable = true;
  oauthSecretFile = "/run/secrets/flox-machines-oauth";
};
```

| Option | Default | Meaning |
|---|---|---|
| `floxMachines.frontDoor.enable` | `false` | run the front door |
| `floxMachines.frontDoor.hostname` | `"machines"` | its tailnet node name |
| `floxMachines.frontDoor.tags` | `[ "tag:flox-machines" ]` | tags it advertises |
| `floxMachines.frontDoor.oauthSecretFile` | `null` | OAuth client secret, or auth key, for its first join |

Without `oauthSecretFile`, the front door prints a Tailscale login URL to its journal until an admin opens it:

```bash
journalctl -u flox-machines-front-door
```

The front door runs as the `flox-machines-front-door` user, in the `kvm` group, and calls the same code as `machine` directly. Its grants cover only what creating and claiming need: a polkit rule to start `microvm@machine-*` units, `zfs allow` on `floxMachines.zfs.parentDataset` for creating zvols, write access to `/var/lib/microvms` and the machines' gcroots, and ownership of the admin SSH key.

A person opens `https://machines.<tailnet>.ts.net`, taps Create, then taps the link to add the machine to their tailnet. Their machine is named after their login: `first.last@example.com` becomes `machine-first-last`.

The tailnet policy needs:

- `tag:flox-machines`, owned by admins, with the OAuth client allowed to create keys for it.
- A grant letting members reach `tag:flox-machines` on port 443, and on port 80 if HTTPS certificates are off.
- A Tailscale SSH rule letting members SSH to their own devices as their own user.
- Device approval turned off, or an admin approving each new machine.

Machines reach Tailscale through the host's NAT, so set `floxMachines.bridge.externalInterface`.

## Host firewall

Machines can reach the host only for DHCP and ping. The module does not trust the bridge, so host services are closed to machines unless they are opened on every interface. Ports in `networking.firewall.allowedTCPPorts` or `allowedUDPPorts`, including the SSH port opened by `services.openssh.openFirewall`, are open on all interfaces, the bridge included; the module warns when any are set. Open host services on the public interface only:

```nix
services.openssh.openFirewall = false;
networking.firewall.interfaces.enp1s0.allowedTCPPorts = [ 22 ];
```

Machines get DNS servers from `floxMachines.bridge.dnsServers` over DHCP, by default `1.1.1.1` and `9.9.9.9`. The host does not answer DNS on the bridge.

## Updates

A host rebuild that changes the template does not restart running machines. Each machine keeps its booted base until it restarts: `machine restart <name>`, `machine resize`, or `sudo reboot` inside the machine. While a newer base is waiting, interactive shells in the machine show:

```
A newer base for this machine is ready. Restart to use it: sudo reboot
```

A machine cannot power itself off. `sudo reboot` and `sudo poweroff` inside a machine both end with the host starting it again on the current base a few seconds later. Stopping a machine is a host action: `machine destroy`, or `systemctl stop microvm@machine-<name>`.

`machine list` shows waiting machines as stale. To restart machines on every template change instead, set `floxMachines.restartOnUpdate = true`.
