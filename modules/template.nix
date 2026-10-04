{
  config,
  lib,
  pkgs,
  inputs,
  floxMachines,
  ...
}:

{
  imports = [
    inputs.flox.nixosModules.flox
    ./user.nix
    ./tailscale.nix
    ./persist.nix
  ];

  microvm = {
    hypervisor = "cloud-hypervisor";
    vcpu = floxMachines.defaults.vcpu;
    mem = floxMachines.defaults.mem;
    socket = "control.socket";
    vsock.cid = 3;
    instance.enable = true;
    interfaces = [
      {
        type = "tap";
        id = "mvm-machine";
        mac = "02:00:00:00:00:00";
      }
    ];
    shares = [
      {
        proto = "virtiofs";
        tag = "ro-store";
        source = "/nix/store";
        mountPoint = "/nix/.ro-store";
        socket = "ro-store.sock";
      }
    ];
    overlayStore = {
      enable = true;
      upperSize = floxMachines.defaults.storeSize;
    };
  };

  networking.hostName = "machine";
  networking.useNetworkd = true;
  networking.useDHCP = false;
  networking.usePredictableInterfaceNames = false;
  systemd.network.networks."10-eth0" = {
    matchConfig.Name = "eth0";
    networkConfig.DHCP = "ipv4";
    dhcpV4Config.ClientIdentifier = "mac";
  };

  services.openssh = {
    enable = true;
    settings = {
      PermitRootLogin = "prohibit-password";
      PasswordAuthentication = false;
    };
  };

  nix = {
    gc = {
      automatic = true;
      dates = "*-*-01,15 03:00";
      randomizedDelaySec = "6h";
      persistent = true;
    };
    settings.experimental-features = [ "nix-command" "flakes" ];
    registry.nixpkgs.flake = inputs.nixpkgs;
    registry.home-manager.flake = inputs.home-manager;
    settings.nix-path = [ "nixpkgs=flake:nixpkgs" ];
  };

  environment.systemPackages = [
    pkgs.git
    pkgs.tmux
    pkgs.home-manager
  ];

  environment.etc."machine/base-version".text = floxMachines.baseVersion;

  environment.interactiveShellInit = ''
    if [ -r /run/microvm/instance/system ] && [ "$(cat /run/microvm/instance/system)" != "$(readlink -f /run/booted-system)" ]; then
      echo "A newer base for this machine is ready. Restart to use it: sudo reboot"
    fi
  '';

  programs.bash.interactiveShellInit = ''
    if [ -n "$SSH_CONNECTION" ] && [ -z "$TMUX" ] && [ "$(id -u)" != 0 ]; then
      exec tmux new-session -A -s main
    fi
  '';

  systemd.services.systemd-reboot.unitConfig.SuccessAction = "poweroff-force";

  documentation.enable = false;
  system.stateVersion = "26.11";
}
