{ config, pkgs, ... }:

let
  instanceDir = config.microvm.instance.mountPoint;
in
{
  users.mutableUsers = true;

  systemd.services.machine-user = {
    description = "Create the machine owner's account from the instance directory";
    wantedBy = [ "multi-user.target" ];
    before = [ "systemd-user-sessions.service" "sshd.service" ];
    unitConfig.RequiresMountsFor = [ instanceDir "/home" ];
    serviceConfig = {
      Type = "oneshot";
      RemainAfterExit = true;
    };
    path = [ pkgs.shadow pkgs.coreutils pkgs.getent ];
    script = ''
      if [ -f ${instanceDir}/authorized_keys ]; then
        install -d -m 0700 /root/.ssh
        install -m 0600 ${instanceDir}/authorized_keys /root/.ssh/authorized_keys
      fi

      [ -f ${instanceDir}/user ] || exit 0
      read -r name < ${instanceDir}/user
      if ! getent passwd "$name" > /dev/null; then
        useradd --uid 1000 --user-group --home-dir "/home/$name" --shell /run/current-system/sw/bin/bash "$name"
      fi
      install -d -o "$name" -g "$name" -m 0700 "/home/$name"
    '';
  };
}
