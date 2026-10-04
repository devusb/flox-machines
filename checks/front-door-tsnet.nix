{
  self,
  nixpkgs,
  system,
}:

{
  front-door-tsnet = import (nixpkgs + "/nixos/tests/make-test-python.nix") (
    { ... }:
    {
      name = "front-door-tsnet";

      nodes.host = {
        imports = [ self.nixosModules.floxMachines ];
        virtualisation.memorySize = 2048;
        floxMachines = {
          enable = true;
          frontDoor.enable = true;
        };
      };

      testScript = /* python */ ''
        host.wait_for_unit("flox-machines-front-door.service", timeout=60)
        host.sleep(30)
        host.succeed("systemctl is-active flox-machines-front-door.service")
        host.succeed("test \"$(systemctl show -p NRestarts --value flox-machines-front-door.service)\" = 0")
        host.succeed("journalctl -u flox-machines-front-door.service | grep -q 'starting tsnet node machines'")
      '';

      meta.timeout = 300;
    }
  ) {
    inherit system;
    pkgs = nixpkgs.legacyPackages.${system};
  };
}
