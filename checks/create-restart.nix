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
          "host"
        ];
        virtualisation.diskSize = 16384;
        virtualisation.memorySize = 6144;
        virtualisation.cores = 4;

        floxMachines = {
          enable = true;
          template = {
            imports = [
              self.nixosModules.machineTemplate
              ./lean-guest.nix
            ];
          };
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
        host.wait_until_succeeds("timeout 10 machine ssh alice true", timeout=300)
        host.wait_until_succeeds("timeout 10 machine ssh bob true", timeout=300)
        host.succeed("echo '9999999999 02:de:ad:be:ef:00 10.100.0.250 machine-bob *' >> /var/lib/dnsmasq/dnsmasq.leases")
        assert host.succeed("timeout 60 machine ssh bob hostname").strip() == "machine-bob"
        assert host.succeed("timeout 60 machine ssh alice hostname").strip() == "machine-alice"
        host.succeed("timeout 60 machine ssh alice id alice")
        host.succeed("timeout 60 machine ssh alice 'runuser -u alice -- sudo -n true'")
        host.succeed("timeout 60 machine ssh alice cat /etc/machine/base-version | grep -qx 1")
        host.succeed("timeout 60 machine ssh alice cat /etc/nix/registry.json | grep -q nixpkgs")
        host.succeed("timeout 60 machine ssh alice cat /etc/nix/registry.json | grep -q home-manager")
        host.succeed("timeout 60 machine ssh alice command -v home-manager")
        host.succeed("timeout 60 machine ssh alice command -v flox")
        host.succeed("timeout 60 machine ssh alice 'runuser -u alice -- sh -c \"echo keep > /home/alice/keep\"'")
        ta = host.succeed("systemctl show -p ActiveEnterTimestampMonotonic microvm@machine-alice.service").strip()
        host.succeed("/run/booted-system/specialisation/v2/bin/switch-to-configuration test")
        host.wait_until_succeeds(f"[ \"$(systemctl show -p ActiveEnterTimestampMonotonic microvm@machine-alice.service)\" != '{ta}' ]", timeout=300)
        host.wait_until_succeeds("timeout 10 machine ssh alice true", timeout=300)
        host.succeed("timeout 60 machine ssh alice cat /etc/machine/base-version | grep -qx 2")
        host.succeed("timeout 60 machine ssh alice cat /home/alice/keep | grep -qx keep")
        host.succeed("machine list | grep -q machine-alice")
        host.succeed("machine create carol")
        host.wait_for_unit("microvm@machine-carol.service")
        host.succeed("machine destroy carol")
        host.fail("systemctl is-active microvm@machine-carol.service")
        host.succeed("test ! -e /var/lib/microvms/machine-carol")
        host.fail("machine create 'Bad Name'")
        host.fail("machine create alice")
        host.succeed("machine resize bob 768 1")
        host.succeed("grep -qx MICROVM_MEM=768 /var/lib/microvms/machine-bob/instance.env")
        host.wait_until_succeeds("tr \\\\0 \\  < /proc/$(systemctl show -p MainPID --value microvm@machine-bob.service)/cmdline | grep -q size=768M", timeout=120)
        host.wait_until_succeeds("timeout 10 machine ssh bob true", timeout=300)
        host.succeed("machine resize bob --reset")
        host.fail("grep -q MICROVM_MEM /var/lib/microvms/machine-bob/instance.env")
        host.wait_until_succeeds("tr \\\\0 \\  < /proc/$(systemctl show -p MainPID --value microvm@machine-bob.service)/cmdline | grep -q size=1024M", timeout=120)
      '';

      meta.timeout = 600;
    }
  ) {
    inherit system;
    pkgs = nixpkgs.legacyPackages.${system};
  };
}
