{ inputs, floxMachines, ... }:

{
  imports = [ inputs.impermanence.nixosModules.impermanence ];

  microvm.volumes = [
    {
      image = "persist.img";
      mountPoint = "/persist";
      size = floxMachines.defaults.persistSize;
      label = "persist";
    }
  ];

  fileSystems."/persist".neededForBoot = true;

  environment.persistence."/persist" = {
    hideMounts = true;
    directories = [
      "/home"
      "/var/log"
      "/var/lib/nixos"
      "/var/lib/systemd/coredump"
      "/var/lib/systemd/timers"
      {
        directory = "/var/lib/tailscale";
        mode = "0700";
      }
    ];
  };

  services.openssh.hostKeys = [
    {
      path = "/persist/etc/ssh/ssh_host_ed25519_key";
      type = "ed25519";
    }
  ];
}
