{
  self,
  nixpkgs,
  system,
}:

{
  create-restart = import (nixpkgs + "/nixos/tests/make-test-python.nix") (
    { ... }:
    {
      name = "create-restart";

      nodes.host = {
        imports = [ self.nixosModules.floxMachines ];

        boot.kernelModules = [ "kvm" ];
        virtualisation.qemu.options = [
          "-cpu"
          "kvm64,+svm,+vmx"
        ];
        virtualisation.diskSize = 16384;
        virtualisation.memorySize = 6144;
        virtualisation.cores = 4;

        floxMachines = {
          enable = true;
          defaults = {
            mem = 1024;
            vcpu = 1;
            homeSize = 512;
            storeSize = 4096;
          };
        };

        specialisation.v2.configuration.floxMachines.baseVersion = "2";
      };

      testScript = /* python */ ''
        host.wait_for_unit("multi-user.target")
        host.succeed("test -L /var/lib/microvms/.templates/machine/current")
        host.succeed("machine create alice")
        host.succeed("machine create bob")
        host.wait_for_unit("microvm@machine-alice.service")
        host.wait_for_unit("microvm@machine-bob.service")
        host.wait_until_succeeds("machine ssh alice true", timeout=300)
        host.wait_until_succeeds("machine ssh bob true", timeout=300)
        assert host.succeed("machine ssh alice hostname").strip() == "machine-alice"
        host.succeed("machine ssh alice id alice")
        host.succeed("machine ssh alice cat /etc/machine/base-version | grep -qx 1")
        host.succeed("machine ssh alice 'su - alice -c \"echo keep > ~/keep\"'")
        ta = host.succeed("systemctl show -p ActiveEnterTimestampMonotonic microvm@machine-alice.service").strip()
        host.succeed("/run/booted-system/specialisation/v2/bin/switch-to-configuration test")
        host.wait_until_succeeds(f"[ \"$(systemctl show -p ActiveEnterTimestampMonotonic microvm@machine-alice.service)\" != '{ta}' ]", timeout=300)
        host.wait_until_succeeds("machine ssh alice true", timeout=300)
        host.succeed("machine ssh alice cat /etc/machine/base-version | grep -qx 2")
        host.succeed("machine ssh alice cat /home/alice/keep | grep -qx keep")
        host.succeed("machine list | grep -q machine-alice")
      '';

      meta.timeout = 3600;
    }
  ) {
    inherit system;
    pkgs = nixpkgs.legacyPackages.${system};
  };
}
