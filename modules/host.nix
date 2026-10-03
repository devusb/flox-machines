{ inputs, self }:

{
  config,
  lib,
  pkgs,
  ...
}:

let
  cfg = config.floxMachines;
  keyDir = "/var/lib/flox-machines";
in
{
  imports = [ inputs.microvm.nixosModules.host ];

  options.floxMachines = {
    enable = lib.mkEnableOption "Flox Machines";

    template = lib.mkOption {
      type = lib.types.deferredModule;
      default = self.nixosModules.machineTemplate;
      description = "Guest NixOS module every machine is built from.";
    };

    baseVersion = lib.mkOption {
      type = lib.types.str;
      default = "1";
      description = "Version string written to /etc/machine/base-version in every machine.";
    };

    storage = lib.mkOption {
      type = lib.types.enum [ "image" "zfs" ];
      default = "image";
      description = "Backing for machine volumes: image files or ZFS zvols.";
    };

    zfs.parentDataset = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Dataset under which machine zvols are created when storage is zfs.";
    };

    defaults = {
      mem = lib.mkOption {
        type = lib.types.int;
        default = 4096;
        description = "Memory in MB for machines without an override.";
      };
      vcpu = lib.mkOption {
        type = lib.types.int;
        default = 2;
        description = "vCPUs for machines without an override.";
      };
      homeSize = lib.mkOption {
        type = lib.types.int;
        default = 20480;
        description = "Home volume size in MB.";
      };
      storeSize = lib.mkOption {
        type = lib.types.int;
        default = 65536;
        description = "Upper Nix store volume size in MB.";
      };
    };

    bridge = {
      name = lib.mkOption {
        type = lib.types.str;
        default = "vmbr0";
        description = "Host bridge machines attach to.";
      };
      address = lib.mkOption {
        type = lib.types.str;
        default = "10.100.0.1";
        description = "Host address on the bridge. The bridge is a /24.";
      };
      externalInterface = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "Interface machines' traffic is masqueraded through. No NAT when null.";
      };
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [ {
      assertion = cfg.storage == "zfs" -> cfg.zfs.parentDataset != null;
      message = "floxMachines.zfs.parentDataset must be set when storage is zfs";
    } ];

    microvm.templates.machine = {
      config = cfg.template;
      specialArgs = {
        inherit inputs;
        floxMachines = cfg;
      };
    };

    systemd.network = {
      enable = true;
      netdevs."10-${cfg.bridge.name}".netdevConfig = {
        Name = cfg.bridge.name;
        Kind = "bridge";
      };
      networks."10-${cfg.bridge.name}" = {
        matchConfig.Name = cfg.bridge.name;
        address = [ "${cfg.bridge.address}/24" ];
        networkConfig.ConfigureWithoutCarrier = true;
      };
      networks."11-machines" = {
        matchConfig.Name = "mvm-*";
        networkConfig.Bridge = cfg.bridge.name;
      };
    };

    services.dnsmasq = {
      enable = true;
      resolveLocalQueries = false;
      settings = {
        interface = cfg.bridge.name;
        bind-dynamic = true;
        dhcp-range = let
          prefix = lib.concatStringsSep "." (lib.take 3 (lib.splitString "." cfg.bridge.address));
        in "${prefix}.10,${prefix}.250,12h";
      };
    };

    networking.firewall.trustedInterfaces = [ cfg.bridge.name ];

    networking.nat = lib.mkIf (cfg.bridge.externalInterface != null) {
      enable = true;
      internalInterfaces = [ cfg.bridge.name ];
      inherit (cfg.bridge) externalInterface;
    };

    systemd.services.flox-machines-key = {
      description = "Generate the Flox Machines admin SSH key";
      wantedBy = [ "multi-user.target" ];
      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = true;
      };
      script = ''
        if [ ! -f ${keyDir}/id_ed25519 ]; then
          install -d -m 0700 ${keyDir}
          ${lib.getExe' pkgs.openssh "ssh-keygen"} -q -t ed25519 -N "" -C flox-machines -f ${keyDir}/id_ed25519
        fi
      '';
    };
  };
}
