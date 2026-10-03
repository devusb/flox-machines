{
  description = "Personal NixOS microVMs from one declared template";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    microvm = {
      url = "git+file:///home/mhelton/code/microvm.nix?ref=instances";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    home-manager = {
      url = "github:nix-community/home-manager";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    flox.url = "github:flox/flox";
  };

  outputs =
    { self, nixpkgs, ... }@inputs:
    let
      system = "x86_64-linux";
    in
    {
      nixosModules = {
        floxMachines = import ./modules/host.nix { inherit inputs self; };
        machineTemplate = import ./modules/template.nix;
      };

      checks.${system} = import ./checks {
        inherit self nixpkgs system;
      };

      legacyPackages.${system}.forkTests =
        let
          args = {
            self = inputs.microvm;
            inherit nixpkgs system;
          };
        in
        import "${inputs.microvm}/checks/instances.nix" args
        // import "${inputs.microvm}/checks/overlay-store.nix" args;
    };
}
