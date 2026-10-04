{
  self,
  nixpkgs,
  system,
}:

let
  pkgs = nixpkgs.legacyPackages.${system};
in
{
  store-reboot = import (nixpkgs + "/nixos/tests/make-test-python.nix") (
    { ... }:
    {
      name = "store-reboot";

      nodes.host = {
        imports = [ self.nixosModules.floxMachines ];

        boot.kernelModules = [ "kvm" ];
        virtualisation.qemu.options = [
          "-cpu"
          "host"
        ];
        virtualisation.diskSize = 16384;
        virtualisation.memorySize = 4096;
        virtualisation.cores = 2;

        system.extraDependencies = [ pkgs.hello ];

        floxMachines = {
          enable = true;
          restartOnUpdate = true;
          template = {
            imports = [
              self.nixosModules.machineTemplate
              ./lean-guest.nix
            ];
          };
          defaults = {
            mem = 1536;
            vcpu = 1;
            persistSize = 512;
            storeSize = 4096;
          };
        };

        specialisation.v2.configuration.floxMachines.baseVersion = "2";
      };

      testScript = /* python */ ''
        def wait_alice():
            host.wait_for_unit("microvm@machine-alice.service")
            host.wait_until_succeeds("timeout 10 machine ssh alice systemctl is-active microvm-verify-store.service", timeout=300)

        host.wait_for_unit("multi-user.target")
        host.succeed("machine create alice")
        wait_alice()

        added = host.succeed("timeout 60 machine ssh alice 'echo local > /home/f && nix store add-file /home/f'").strip()

        def check():
            host.succeed(f"timeout 60 machine ssh alice nix path-info {added} ${pkgs.hello}")
            host.succeed("timeout 60 machine ssh alice nix-store --verify")

        check()

        with subtest("restart"):
            host.succeed("machine restart alice")
            wait_alice()
            check()

        with subtest("base update"):
            started = host.succeed("systemctl show -p ActiveEnterTimestampMonotonic microvm@machine-alice.service").strip()
            host.succeed("/run/booted-system/specialisation/v2/bin/switch-to-configuration test")
            host.wait_until_succeeds(f"[ \"$(systemctl show -p ActiveEnterTimestampMonotonic microvm@machine-alice.service)\" != '{started}' ]", timeout=300)
            wait_alice()
            host.succeed("timeout 60 machine ssh alice cat /etc/machine/base-version | grep -qx 2")
            check()

        with subtest("machine gc"):
            host.succeed("machine gc")
            wait_alice()
            check()

        with subtest("reimage clears the store layer and keeps home"):
            host.succeed("machine reimage alice")
            wait_alice()
            host.succeed("timeout 60 machine ssh alice cat /home/f | grep -qx local")
            host.fail(f"timeout 60 machine ssh alice nix path-info {added}")
            host.succeed("timeout 60 machine ssh alice nix-store --verify")
      '';

      meta.timeout = 600;
    }
  ) {
    inherit system pkgs;
  };
}
