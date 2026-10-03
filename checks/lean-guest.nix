{ lib, ... }:

{
  documentation.enable = false;
  services.timesyncd.enable = false;
  services.logrotate.enable = false;
  services.fstrim.enable = false;
  nix.settings = {
    substituters = lib.mkForce [ ];
    connect-timeout = 1;
  };
}
