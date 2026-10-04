{
  self,
  nixpkgs,
  system,
}:

let
  args = { inherit self nixpkgs system; };
in
import ./create-restart.nix args
// import ./store-reboot.nix args
// import ./zfs-backend.nix args
// import ./user-units.nix args
// import ./front-door.nix args
// import ./front-door-tsnet.nix args
// import ./network-isolation.nix args
// import ./host-gc-refused.nix args
