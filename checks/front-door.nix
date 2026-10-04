{
  self,
  nixpkgs,
  system,
}:

{
  front-door = import (nixpkgs + "/nixos/tests/make-test-python.nix") (
    { ... }:
    {
      name = "front-door";

      nodes.host = {
        imports = [
          self.nixosModules.floxMachines
          ({ pkgs, ... }: { environment.systemPackages = [ pkgs.curl ]; })
        ];

        boot.kernelModules = [ "kvm" ];
        virtualisation.qemu.options = [
          "-cpu"
          "host"
        ];
        virtualisation.diskSize = 8192;
        virtualisation.memorySize = 4096;
        virtualisation.cores = 4;

        floxMachines = {
          enable = true;
          frontDoor = {
            enable = true;
            testListen = "127.0.0.1:8080";
          };
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
        import json

        host.wait_for_unit("multi-user.target")
        host.succeed("machine create alice --owner alice@example.com")
        host.succeed("test \"$(cat /var/lib/microvms/machine-alice/owner)\" = alice@example.com")
        host.succeed("test \"$(stat -c %a /var/lib/microvms/machine-alice/owner)\" = 600")
        host.wait_until_succeeds("timeout 10 machine ssh alice true", timeout=300)

        s = json.loads(host.succeed("timeout 30 machine status alice --json"))
        assert s["exists"] and s["owner"] == "alice@example.com" and s["running"] and s["reachable"], s
        assert s["tailscale"]["state"] == "NeedsLogin", s

        host.succeed("timeout 20 machine login alice")
        host.wait_until_succeeds("timeout 10 machine ssh alice systemctl is-active machine-tailscale-login.service", timeout=60)

        assert json.loads(host.succeed("timeout 30 machine status nobody-here --json")) == {"name": "nobody-here", "exists": False}
        host.fail("machine create root")
        host.fail("machine create admin")

        host.wait_for_unit("flox-machines-front-door.service")
        host.succeed("test \"$(systemctl show -p User --value flox-machines-front-door.service)\" = flox-machines-front-door")
        host.wait_for_open_port(8080)
        host.succeed("curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/ | grep -qx 403")
        import re
        page = host.succeed("curl -s -H 'X-Test-Login: bob@example.com' http://127.0.0.1:8080/")
        match = re.search(r'name="token" value="([0-9a-f]+)"', page)
        assert match, page
        token = match.group(1)
        host.succeed(f"curl -s -o /dev/null -H 'X-Test-Login: bob@example.com' -d token={token} http://127.0.0.1:8080/create")
        host.succeed("test \"$(cat /var/lib/microvms/machine-bob/owner)\" = bob@example.com")
      '';

      meta.timeout = 600;
    }
  ) {
    inherit system;
    pkgs = nixpkgs.legacyPackages.${system};
  };
}
