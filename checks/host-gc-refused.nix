{
  self,
  nixpkgs,
  system,
}:

let
  pkgs = nixpkgs.legacyPackages.${system};
  failed =
    extra:
    let
      host = nixpkgs.lib.nixosSystem {
        inherit system;
        modules = [
          self.nixosModules.floxMachines
          { floxMachines.enable = true; }
          extra
        ];
      };
    in
    map (a: a.message) (builtins.filter (a: !a.assertion) host.config.assertions);
  mentionsGC = messages: builtins.any (m: nixpkgs.lib.hasInfix "machine gc" m) messages;
  cases = {
    plain = !mentionsGC (failed { });
    automatic = mentionsGC (failed {
      nix.gc.automatic = true;
    });
    min-free = mentionsGC (failed {
      nix.settings.min-free = 1024;
    });
  };
in
{
  host-gc-refused = pkgs.runCommand "host-gc-refused" { } (
    pkgs.lib.concatStrings (
      pkgs.lib.mapAttrsToList (
        name: ok: if ok then "" else "echo 'case ${name} failed' >&2; exit 1\n"
      ) cases
    )
    + "touch $out\n"
  );
}
