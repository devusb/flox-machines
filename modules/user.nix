{ config, pkgs, ... }:

let
  instanceDir = config.microvm.instance.mountPoint;
  shellFile = "/persist/etc/machine/shell";
in
{
  users.mutableUsers = true;
  security.sudo.wheelNeedsPassword = false;

  systemd.services.machine-user = {
    description = "Create the machine owner's account from the instance directory";
    wantedBy = [ "multi-user.target" ];
    before = [
      "systemd-user-sessions.service"
      "sshd.service"
    ];
    unitConfig.RequiresMountsFor = [
      instanceDir
      "/home"
    ];
    serviceConfig = {
      Type = "oneshot";
      RemainAfterExit = true;
    };
    path = [
      pkgs.shadow
      pkgs.coreutils
      pkgs.getent
    ];
    script = ''
      if [ -f ${instanceDir}/authorized_keys ]; then
        install -d -m 0700 /root/.ssh
        install -m 0600 ${instanceDir}/authorized_keys /root/.ssh/authorized_keys
      fi

      [ -f ${instanceDir}/user ] || exit 0
      read -r name < ${instanceDir}/user
      shell=/run/current-system/sw/bin/bash
      if [ -r ${shellFile} ]; then
        read -r saved < ${shellFile} || true
        case "$saved" in
          /*) [ -x "$saved" ] && shell=$saved ;;
        esac
      fi
      if ! getent passwd "$name" > /dev/null; then
        useradd --uid 1000 --user-group --groups wheel --home-dir "/home/$name" --shell "$shell" "$name"
      fi
      install -d -o "$name" -g "$name" -m 0700 "/home/$name"
    '';
  };

  systemd.paths.machine-shell-save = {
    description = "Watch the machine owner's login shell";
    wantedBy = [ "multi-user.target" ];
    after = [ "machine-user.service" ];
    requires = [ "machine-user.service" ];
    before = [
      "multi-user.target"
      "shutdown.target"
    ];
    conflicts = [ "shutdown.target" ];
    unitConfig = {
      DefaultDependencies = false;
      ConditionPathExists = "${instanceDir}/user";
    };
    pathConfig.PathChanged = "/etc/passwd";
  };

  systemd.services.machine-shell-save = {
    description = "Save the machine owner's login shell";
    unitConfig.RequiresMountsFor = [ "/persist" ];
    serviceConfig.Type = "oneshot";
    path = [
      pkgs.coreutils
      pkgs.getent
    ];
    script = ''
      read -r name < ${instanceDir}/user
      shell=$(getent passwd "$name" | cut -d: -f7)
      [ -n "$shell" ] || exit 0
      install -d -m 0755 ${dirOf shellFile}
      printf '%s\n' "$shell" > ${shellFile}.new
      mv -f ${shellFile}.new ${shellFile}
    '';
  };

  systemd.services.machine-linger = {
    description = "Start the machine owner's user services";
    wantedBy = [ "multi-user.target" ];
    after = [
      "machine-user.service"
      "microvm-verify-store.service"
      "systemd-logind.service"
    ];
    requires = [ "machine-user.service" ];
    wants = [ "systemd-logind.service" ];
    unitConfig.ConditionPathExists = "${instanceDir}/user";
    serviceConfig = {
      Type = "oneshot";
      RemainAfterExit = true;
    };
    path = [ config.systemd.package ];
    script = ''
      read -r name < ${instanceDir}/user
      loginctl enable-linger "$name"
    '';
  };
}
