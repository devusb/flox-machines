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
    treefmt-nix = {
      url = "github:numtide/treefmt-nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs =
    { self, nixpkgs, ... }@inputs:
    let
      system = "x86_64-linux";
      pkgs = nixpkgs.legacyPackages.${system};
      treefmt = inputs.treefmt-nix.lib.evalModule pkgs {
        projectRootFile = "flake.nix";
        programs.nixfmt.enable = true;
        programs.gofmt.enable = true;
        programs.yamlfmt.enable = true;
        settings.excludes = [
          ".gitignore"
          "flake.lock"
          "go.sum"
        ];
      };
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
        )
        // {
          formatting = treefmt.config.build.check self;
        };

      formatter.${system} = treefmt.config.build.wrapper;

      packages.${system}.flox-machines =
        nixpkgs.legacyPackages.${system}.callPackage ./pkgs/flox-machines.nix
          { };
    };
}
