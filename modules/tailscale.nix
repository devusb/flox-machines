{
  services.tailscale = {
    enable = true;
    extraSetFlags = [ "--ssh" ];
  };

  networking.firewall.trustedInterfaces = [ "tailscale0" ];

  systemd.services.tailscaled.unitConfig.RequiresMountsFor = [ "/var/lib/tailscale" ];
}
