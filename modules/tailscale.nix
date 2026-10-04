{
  services.tailscale = {
    enable = true;
    extraSetFlags = [ "--ssh" ];
  };

  networking.firewall.trustedInterfaces = [ "tailscale0" ];

  systemd.tmpfiles.rules = [ "d /var/lib/machine/tailscale 0700 root root -" ];

  systemd.services.tailscaled = {
    unitConfig.RequiresMountsFor = [ "/var/lib/machine" ];
    serviceConfig.BindPaths = [ "/var/lib/machine/tailscale:/var/lib/tailscale" ];
  };
}
