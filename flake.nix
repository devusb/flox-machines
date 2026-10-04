{
  description = "Personal NixOS microVMs from one declared template";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    microvm = {
      url = "github:devusb/microvm.nix/instances";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    home-manager = {
      url = "github:nix-community/home-manager";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    flox.url = "github:flox/flox/latest";
    impermanence.url = "github:nix-community/impermanence";
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

      checks.${system} =
        import ./checks { inherit self nixpkgs system; }
        // nixpkgs.lib.mapAttrs' (name: nixpkgs.lib.nameValuePair "fork-${name}") (
          let
            args = {
              self = inputs.microvm;
              inherit nixpkgs system;
            };
          in
          import "${inputs.microvm}/checks/instances.nix" args
          // import "${inputs.microvm}/checks/overlay-store.nix" args
        );

      packages.${system}.front-door = nixpkgs.legacyPackages.${system}.callPackage ./pkgs/front-door.nix { };
    };
}
