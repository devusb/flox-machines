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
  machinesPackage = pkgs.callPackage ../pkgs/flox-machines.nix { };
  frontDoorUser = "flox-machines-front-door";
  frontDoorMachine = pkgs.writeShellScript "front-door-machine" ''
    exec /run/wrappers/bin/sudo -n ${lib.getExe' machinesPackage "machine"} "$@"
  '';
in
{
  imports = [
    inputs.microvm.nixosModules.host
    inputs.flox.nixosModules.flox
  ];

  options.floxMachines = {
    enable = lib.mkEnableOption "Flox Machines";

    template = lib.mkOption {
      type = lib.types.deferredModule;
      default = self.nixosModules.machineTemplate;
      description = "Guest NixOS module every machine is built from.";
    };


    restartOnUpdate = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = ''
        Restart running machines when a host rebuild changes the template.
        When false, running machines keep their booted base until they are
        restarted, and show a notice at login that a newer base is waiting.
      '';
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
    }
    {
      assertion = cfg.frontDoor.oauthSecretFile == null || !lib.hasPrefix "${builtins.storeDir}/" cfg.frontDoor.oauthSecretFile;
      message = "floxMachines.frontDoor.oauthSecretFile must not be in the Nix store, where every user can read it";
    } ];

    microvm.templates.machine = {
      config = cfg.template;
      restartIfChanged = cfg.restartOnUpdate;
      specialArgs = {
        inherit inputs;
        floxMachines = cfg;
      };
    };

    networking.useNetworkd = lib.mkDefault true;

    systemd.network = {
      enable = true;
      wait-online.ignoredInterfaces = [ cfg.bridge.name ];
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

    environment.systemPackages = [ machinesPackage ];

    environment.etc."flox-machines/config.json".text = builtins.toJSON {
      stateDir = "/var/lib/microvms";
      inherit (cfg) storage;
      parentDataset = toString cfg.zfs.parentDataset;
      inherit (cfg.defaults) persistSize;
      keyPath = "${keyDir}/id_ed25519";
      reservedNames = builtins.attrNames config.microvm.templates.machine.config.config.users.users;
    };

    boot.supportedFilesystems = lib.mkIf (cfg.storage == "zfs") [ "zfs" ];

    services.udev.extraRules = lib.mkIf (cfg.storage == "zfs") ''
      SUBSYSTEM=="block", KERNEL=="zd*", GROUP="kvm", MODE="0660"
    '';

    services.zfs.autoSnapshot.enable = lib.mkIf (cfg.storage == "zfs") true;



    users.users.${frontDoorUser} = lib.mkIf cfg.frontDoor.enable {
      isSystemUser = true;
      group = frontDoorUser;
    };
    users.groups.${frontDoorUser} = lib.mkIf cfg.frontDoor.enable { };

    security.sudo.extraRules = lib.mkIf cfg.frontDoor.enable [
      {
        users = [ frontDoorUser ];
        commands = map (command: {
          command = "${lib.getExe' machinesPackage "machine"} ${command} *";
          options = [ "NOPASSWD" ];
        }) [ "create" "status" "login" ];
      }
    ];
    systemd.services.flox-machines-front-door = lib.mkIf cfg.frontDoor.enable {
      description = "Flox Machines front door";
      wantedBy = [ "multi-user.target" ];
      after = [ "flox-machines-key.service" ];
      path = [ machinesPackage "/run/current-system/sw" ];
      serviceConfig = {
        ExecStart = lib.escapeShellArgs (
          [
            (lib.getExe' machinesPackage "flox-machines-front-door")
            "--hostname"
            cfg.frontDoor.hostname
            "--tags"
            (lib.concatStringsSep "," cfg.frontDoor.tags)
            "--machine"
            "${frontDoorMachine}"
            "--state-dir"
            "/var/lib/flox-machines-front-door"
          ]
          ++ lib.optionals (cfg.frontDoor.oauthSecretFile != null) [ "--secret-file" "%d/secret" ]
          ++ lib.optionals (cfg.frontDoor.testListen != null) [ "--test-listen" cfg.frontDoor.testListen ]
        );
        LoadCredential = lib.optional (cfg.frontDoor.oauthSecretFile != null) "secret:${cfg.frontDoor.oauthSecretFile}";
        User = frontDoorUser;
        Group = frontDoorUser;
        StateDirectory = "flox-machines-front-door";
        StateDirectoryMode = "0700";
        Restart = "on-failure";
        RestartSec = "5s";
      };
    };

    systemd.services.flox-machines-base-marker = {
      description = "Tell machines which base the template currently builds";
      wantedBy = [ "microvms.target" ];
      restartTriggers = [ config.microvm.templates.machine.config.config.system.build.toplevel ];
      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = true;
      };
      script = ''
        for d in /var/lib/microvms/machine-*; do
          [ -d "$d/instance" ] || continue
          echo ${config.microvm.templates.machine.config.config.system.build.toplevel} > "$d/instance/system.new" && mv -f "$d/instance/system.new" "$d/instance/system" || true
        done
      '';
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
