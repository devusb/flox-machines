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
    ./user.nix
    ./tailscale.nix
  ];

  microvm = {
    hypervisor = "cloud-hypervisor";
    vcpu = floxMachines.defaults.vcpu;
    mem = floxMachines.defaults.mem;
    socket = "control.socket";
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
    volumes = [
      {
        image = "home.img";
        mountPoint = "/home";
        size = floxMachines.defaults.homeSize;
        label = "home";
      }
      {
        image = "state.img";
        mountPoint = "/var/lib/machine";
        size = 1024;
        label = "state";
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
    settings.experimental-features = [ "nix-command" "flakes" ];
    registry.nixpkgs.flake = inputs.nixpkgs;
    registry.home-manager.flake = inputs.home-manager;
    nixPath = [ "nixpkgs=flake:nixpkgs" ];
  };

  environment.systemPackages = [
    pkgs.git
    pkgs.tmux
    pkgs.home-manager
    inputs.flox.packages.${pkgs.stdenv.hostPlatform.system}.default
  ];

  environment.etc."machine/base-version".text = floxMachines.baseVersion;

  programs.bash.interactiveShellInit = ''
    if [ -n "$SSH_CONNECTION" ] && [ -z "$TMUX" ] && [ "$(id -u)" != 0 ]; then
      exec tmux new-session -A -s main
    fi
  '';

  documentation.enable = false;
  system.stateVersion = "26.11";
}
