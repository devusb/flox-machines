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
  machineCli = pkgs.callPackage ../pkgs/machine-cli.nix {
    inherit (cfg) storage;
    inherit (cfg.defaults) persistSize;
    parentDataset = cfg.zfs.parentDataset;
    inherit keyDir;
  };
  frontDoorPackage = pkgs.callPackage ../pkgs/front-door.nix { };
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
      persistSize = lib.mkOption {
        type = lib.types.int;
        default = 20480;
        description = "Persistent volume size in MB. Holds /home and machine state.";
      };
      storeSize = lib.mkOption {
        type = lib.types.int;
        default = 65536;
        description = "Upper Nix store volume size in MB.";
      };
    };


    frontDoor = {
      enable = lib.mkEnableOption "the front door web service";

      hostname = lib.mkOption {
        type = lib.types.str;
        default = "machines";
        description = "Tailnet node name of the front door.";
      };

      tags = lib.mkOption {
        type = lib.types.listOf lib.types.str;
        default = [ "tag:flox-machines" ];
        description = "Tags the front door node advertises.";
      };

      oauthSecretFile = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = ''
          File holding a Tailscale OAuth client secret or auth key for the
          front door's first join. Without it, the front door prints a login
          URL to its journal.
        '';
      };

      testListen = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        internal = true;
        description = "For tests only: serve plain HTTP on this address with identity from a request header.";
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
      dnsServers = lib.mkOption {
        type = lib.types.listOf lib.types.str;
        default = [
          "1.1.1.1"
          "9.9.9.9"
        ];
        description = "DNS servers handed to machines over DHCP.";
      };

      externalInterface = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "Interface machines' traffic is masqueraded through. No NAT when null.";
      };
    };
  };

  config = lib.mkIf cfg.enable {
    warnings = lib.optional (config.networking.firewall.allowedTCPPorts != [ ] || config.networking.firewall.allowedUDPPorts != [ ]) "floxMachines: ports opened in networking.firewall.allowedTCPPorts or allowedUDPPorts are reachable from machines. Open host services per interface with networking.firewall.interfaces.<name> instead.";

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
        bridgeConfig.Isolated = true;
      };
    };

    services.dnsmasq = {
      enable = true;
      resolveLocalQueries = false;
      settings = {
        interface = cfg.bridge.name;
        bind-dynamic = true;
        port = 0;
        dhcp-option = [ "option:dns-server,${lib.concatStringsSep "," cfg.bridge.dnsServers}" ];
        dhcp-range = let
          prefix = lib.concatStringsSep "." (lib.take 3 (lib.splitString "." cfg.bridge.address));
        in "${prefix}.10,${prefix}.250,12h";
      };
    };

    networking.firewall.interfaces.${cfg.bridge.name}.allowedUDPPorts = [ 67 ];

    networking.nat = lib.mkIf (cfg.bridge.externalInterface != null) {
      enable = true;
      internalInterfaces = [ cfg.bridge.name ];
      inherit (cfg.bridge) externalInterface;
    };

    environment.systemPackages = [ machineCli ];

    boot.supportedFilesystems = lib.mkIf (cfg.storage == "zfs") [ "zfs" ];

    services.udev.extraRules = lib.mkIf (cfg.storage == "zfs") ''
      SUBSYSTEM=="block", KERNEL=="zd*", GROUP="kvm", MODE="0660"
    '';

    services.zfs.autoSnapshot.enable = lib.mkIf (cfg.storage == "zfs") true;


    systemd.services.flox-machines-front-door = lib.mkIf cfg.frontDoor.enable {
      description = "Flox Machines front door";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" "flox-machines-key.service" ];
      wants = [ "network-online.target" ];
      path = [ machineCli "/run/current-system/sw" ];
      serviceConfig = {
        ExecStart = lib.escapeShellArgs (
          [
            (lib.getExe frontDoorPackage)
            "--hostname"
            cfg.frontDoor.hostname
            "--tags"
            (lib.concatStringsSep "," cfg.frontDoor.tags)
            "--machine"
            (lib.getExe' machineCli "machine")
          ]
          ++ lib.optionals (cfg.frontDoor.oauthSecretFile != null) [ "--secret-file" "%d/secret" ]
          ++ lib.optionals (cfg.frontDoor.testListen != null) [ "--test-listen" cfg.frontDoor.testListen ]
        );
        LoadCredential = lib.optional (cfg.frontDoor.oauthSecretFile != null) "secret:${cfg.frontDoor.oauthSecretFile}";
        StateDirectory = "flox-machines/front-door";
        StateDirectoryMode = "0700";
        Restart = "on-failure";
        RestartSec = "5s";
      };
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
