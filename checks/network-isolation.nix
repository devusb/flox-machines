{
  self,
  nixpkgs,
  system,
}:

{
  network-isolation = import (nixpkgs + "/nixos/tests/make-test-python.nix") (
    { ... }:
    {
      name = "network-isolation";

      nodes.host = {
        imports = [ self.nixosModules.floxMachines ];

        boot.kernelModules = [ "kvm" ];
        virtualisation.qemu.options = [
          "-cpu"
          "host"
        ];
        virtualisation.diskSize = 8192;
        virtualisation.memorySize = 4096;
        virtualisation.cores = 4;

        services.openssh = {
          enable = true;
          openFirewall = false;
        };
        networking.firewall.interfaces.eth1.allowedTCPPorts = [ 22 ];

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
            persistSize = 512;
            storeSize = 2048;
          };
        };
      };

      testScript = /* python */ ''
        def on(name, cmd):
            return host.succeed(f"timeout 60 machine ssh {name} '{cmd}'")

        def fails_on(name, cmd):
            host.fail(f"timeout 60 machine ssh {name} '{cmd}'")

        host.wait_for_unit("multi-user.target")
        host.wait_for_unit("sshd.service")
        host.succeed("machine create alice")
        host.succeed("machine create bob")
        host.wait_until_succeeds("timeout 10 machine ssh alice true", timeout=300)
        host.wait_until_succeeds("timeout 10 machine ssh bob true", timeout=300)

        with subtest("guests reach the host only for DHCP"):
            on("alice", "ping -c1 -W2 10.100.0.1")
            fails_on("alice", "timeout 5 bash -c \"</dev/tcp/10.100.0.1/22\"")

        with subtest("guests cannot reach each other over the bridge"):
            bob_ip = on("bob", "ip -4 -o addr show dev eth0 | awk \"{print \\$4}\" | cut -d/ -f1").strip()
            fails_on("alice", f"ping -c1 -W2 {bob_ip}")
      '';

      meta.timeout = 600;
    }
  ) {
    inherit system;
    pkgs = nixpkgs.legacyPackages.${system};
  };
}
