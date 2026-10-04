{ self, nixpkgs, system }:

let
  pkgs = nixpkgs.legacyPackages.${system};
  cases = {
    tailscale-running = {
      state = "Running";
      authURL = "";
      dnsName = "machine-alice.example.ts.net";
      owner = "alice@example.com";
    };
    tailscale-needslogin-url = {
      state = "NeedsLogin";
      authURL = "https://login.tailscale.com/a/abc123";
      dnsName = "";
      owner = "";
    };
    tailscale-needslogin = {
      state = "NeedsLogin";
      authURL = "";
      dnsName = "";
      owner = "";
    };
  };
in
{
  tailscale-status-jq = pkgs.runCommand "tailscale-status-jq" { nativeBuildInputs = [ pkgs.jq ]; } (
    pkgs.lib.concatStrings (
      pkgs.lib.mapAttrsToList (name: expected: ''
        got=$(jq -S -c -f ${../pkgs/tailscale-status.jq} ${./fixtures}/${name}.json)
        want=$(echo ${pkgs.lib.escapeShellArg (builtins.toJSON expected)} | jq -S -c .)
        if [ "$got" != "$want" ]; then
          echo "${name}: got $got, want $want" >&2
          exit 1
        fi
      '') cases
    )
    + "touch $out\n"
  );
}
