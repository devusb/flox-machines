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
| `floxMachines.defaults.persistSize` | `20480` | persistent volume in MB, holding `/home` and machine state |
| `floxMachines.defaults.storeSize` | `65536` | Nix store upper layer in MB |
| `floxMachines.storage` | `"image"` | `"image"` or `"zfs"` |
| `floxMachines.zfs.parentDataset` | `null` | dataset for machine zvols when storage is `zfs` |
| `floxMachines.bridge.externalInterface` | `null` | interface machines are NATed through |

## Commands

| Command | Effect |
|---|---|
| `machine create <name>` | create and start `machine-<name>` with user `<name>` |
| `machine ssh <name> [command]` | run a command as root on the machine |
| `machine restart <name>` | restart the machine |
| `machine resize <name> <mem-MB> <vcpu>` | set a per-machine size and restart |
| `machine resize <name> --reset` | return to the template size and restart |
| `machine reimage <name>` | wipe the machine's Nix store layer and Nix state, then restart; home is kept, but home-manager generations and `nix profile` installs are removed, so the person runs `home-manager switch` again |
| `machine destroy <name>` | stop the machine and delete it with its volumes |
| `machine list` | list machines and whether they run the current base |
| `machine gc` | stop all machines, collect garbage on the host, start them again |

A host rebuild that changes the template restarts every machine onto the new base. A rebuild that does not change it restarts nothing.

## Store

Each machine's Nix store layers its own persistent upper layer over the host's store, which it reads but never writes. Paths a person installs survive restarts and base updates. Host garbage collection runs only through `machine gc`. Each machine repairs missing referenced paths from its substituters at boot.

## Storage backends

Each machine has one persistent volume mounted at `/persist`. The guest root is tmpfs. These paths live on `/persist` through the impermanence module: `/home`, `/var/log`, `/var/lib/nixos`, `/var/lib/systemd/coredump`, `/var/lib/systemd/timers` and `/var/lib/tailscale`. The SSH host key is kept in `/persist/etc/ssh`. The Nix store layers are separate image files that `machine reimage` deletes.

Anything written elsewhere, including system changes made with sudo, resets when the machine restarts. Keep what should last in home, in Nix or Flox environments, or add it to the template's persistence list.

With `storage = "image"`, the persistent volume is `persist.img` under `/var/lib/microvms/machine-<name>/`. With `storage = "zfs"`, it is the zvol `<parentDataset>/<name>`, auto-snapshotted. Snapshot a machine with `zfs snapshot <parentDataset>/<name>@<label>`; roll back with the machine stopped.

## Tests

Tests are NixOS tests that run nested VMs. Build them on a machine with KVM:

```bash
nix build -L .#checks.x86_64-linux.create-restart
nix build -L .#checks.x86_64-linux.store-reboot
```
