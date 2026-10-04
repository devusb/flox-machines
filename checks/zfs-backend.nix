{
  self,
  nixpkgs,
  system,
}:

{
  zfs-backend = import (nixpkgs + "/nixos/tests/make-test-python.nix") (
    { ... }:
    {
      name = "zfs-backend";

      nodes.host = {
        imports = [ self.nixosModules.floxMachines ];

        boot.kernelModules = [ "kvm" ];
        virtualisation.qemu.options = [
          "-cpu"
          "host"
        ];
        virtualisation.diskSize = 8192;
        virtualisation.memorySize = 4096;
        virtualisation.cores = 2;
        virtualisation.emptyDiskImages = [ 4096 ];
        networking.hostId = "8425e349";

        floxMachines = {
          enable = true;
          storage = "zfs";
          zfs.parentDataset = "tank/machines";
          template = {
            imports = [
              self.nixosModules.machineTemplate
              ./lean-guest.nix
            ];
          };
          defaults = {
            mem = 1024;
            vcpu = 1;
            persistSize = 512;
            storeSize = 2048;
          };
        };
      };

      testScript = /* python */ ''
        host.wait_for_unit("multi-user.target")
        host.succeed("zpool create tank /dev/vdb && zfs create tank/machines")
        host.succeed("machine create alice")
        host.succeed("zfs list -H -o name | grep -qx tank/machines/alice")
        host.succeed("test -L /var/lib/microvms/machine-alice/persist.img")
        host.wait_until_succeeds("timeout 10 machine ssh alice true", timeout=300)
        host.succeed("timeout 60 machine ssh alice findmnt -n -o SOURCE /persist | grep -q /dev/vd")
        host.succeed("timeout 60 machine ssh alice 'echo before > /home/alice/z && sync'")

        host.succeed("zfs snapshot tank/machines/alice@test")
        host.succeed("timeout 60 machine ssh alice 'echo after > /home/alice/z && sync'")
        host.succeed("systemctl stop microvm@machine-alice.service")
        host.succeed("zfs rollback tank/machines/alice@test")
        host.succeed("systemctl start microvm@machine-alice.service")
        host.wait_until_succeeds("timeout 10 machine ssh alice true", timeout=300)
        host.succeed("timeout 60 machine ssh alice cat /home/alice/z | grep -qx before")

        host.succeed("machine destroy alice")
        host.fail("zfs list -H -o name | grep -q tank/machines/alice")
      '';

      meta.timeout = 600;
    }
  ) {
    inherit system;
    pkgs = nixpkgs.legacyPackages.${system};
  };
}
