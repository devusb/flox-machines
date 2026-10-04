{ config, pkgs, ... }:

let
  instanceDir = config.microvm.instance.mountPoint;
  stateDir = "/var/lib/machine/tailscale";
in
{
  services.tailscale.enable = true;

  networking.firewall.trustedInterfaces = [ "tailscale0" ];

  systemd.tmpfiles.rules = [ "d ${stateDir} 0700 root root -" ];

  systemd.services.tailscaled = {
    unitConfig.RequiresMountsFor = [ "/var/lib/machine" ];
    serviceConfig.BindPaths = [ "${stateDir}:/var/lib/tailscale" ];
  };

  systemd.services.machine-tailscale = {
    description = "Make the machine owner the Tailscale operator";
    wantedBy = [ "multi-user.target" ];
    after = [ "tailscaled.service" "machine-user.service" ];
    requires = [ "tailscaled.service" "machine-user.service" ];
    unitConfig.ConditionPathExists = "${instanceDir}/user";
    serviceConfig = {
      Type = "oneshot";
      RemainAfterExit = true;
    };
    path = [ config.services.tailscale.package ];
    script = ''
      read -r name < ${instanceDir}/user
      tailscale set --operator="$name" --ssh
    '';
  };
}
