{ self, nixpkgs, system }:

let
  args = { inherit self nixpkgs system; };
in
import ./create-restart.nix args
// import ./store-reboot.nix args
