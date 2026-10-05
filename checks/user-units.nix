{
  self,
  nixpkgs,
  system,
}:

{
  user-units =
    import (nixpkgs + "/nixos/tests/make-test-python.nix")
      (
        { ... }:
        {
          name = "user-units";

          nodes.host = {
            imports = [ self.nixosModules.floxMachines ];

            boot.kernelModules = [ "kvm" ];
            virtualisation.qemu.options = [
              "-cpu"
              "host"
            ];
            virtualisation.diskSize = 8192;
            virtualisation.memorySize = 3072;
            virtualisation.cores = 2;

            floxMachines = {
              enable = true;
              template = {
                imports = [
                  self.nixosModules.machineTemplate
                  ./lean-guest.nix
                ];
              };
              defaults = {
                mem = 1024;
                vcpu = 1;
                persistSize = 512;
                storeSize = 2048;
              };
            };
          };

          testScript = /* python */ ''
            unit = """[Unit]
            Description=marker

            [Service]
            Type=oneshot
            RemainAfterExit=yes
            ExecStart=/bin/sh -c 'date > %h/marker-ran'

            [Install]
            WantedBy=default.target
            """

            host.wait_for_unit("multi-user.target")
            host.succeed("machine create alice")
            host.wait_until_succeeds("timeout 10 machine ssh alice systemctl is-active machine-user.service", timeout=300)
            host.fail("timeout 60 machine ssh alice 'journalctl -b -o cat | grep -qi \"ordering cycle\"'")
            host.succeed("timeout 60 machine ssh alice systemctl is-active machine-shell-save.path")

            import base64
            encoded = base64.b64encode(unit.encode()).decode()
            host.succeed(
                "timeout 60 machine ssh alice '"
                "d=/home/alice/.config/systemd/user; "
                "mkdir -p $d/default.target.wants && "
                f"echo {encoded} | base64 -d > $d/marker.service && "
                "ln -sf ../marker.service $d/default.target.wants/marker.service && "
                "chown -R alice:alice /home/alice/.config'"
            )

            fish = "/run/current-system/sw/bin/fish"
            host.succeed(f"timeout 60 machine ssh alice chsh -s {fish} alice")
            host.wait_until_succeeds(f"timeout 10 machine ssh alice grep -qx {fish} /persist/etc/machine/shell", timeout=30)

            host.succeed("machine restart alice")
            host.wait_until_succeeds("timeout 10 machine ssh alice systemctl is-active machine-user.service", timeout=300)
            assert host.succeed("timeout 60 machine ssh alice getent passwd alice").strip().endswith(":" + fish), "shell did not survive a restart"
            host.wait_until_succeeds("timeout 10 machine ssh alice test -f /home/alice/marker-ran", timeout=120)
            host.succeed("timeout 60 machine ssh alice loginctl show-user alice --property=Linger | grep -qx Linger=yes")
            import json
            boots = json.loads(host.succeed("timeout 60 machine ssh alice journalctl --list-boots -o json"))
            assert len(boots) >= 2, f"journal did not keep the previous boot: {boots}"
            def started_at(unit):
                return int(host.succeed(f"timeout 60 machine ssh alice systemctl show -p ActiveEnterTimestampMonotonic --value {unit}").strip())
            assert started_at("user@1000.service") >= started_at("microvm-verify-store.service"), "user manager started before the store repair finished"
            import json
            sessions = json.loads(host.succeed("timeout 60 machine ssh alice loginctl list-sessions --json=short"))
            assert not [s for s in sessions if s.get("user") == "alice" and s.get("class") != "manager"], sessions

            host.succeed("timeout 60 machine ssh alice 'echo /nonexistent > /persist/etc/machine/shell'")
            host.succeed("machine restart alice")
            host.wait_until_succeeds("timeout 10 machine ssh alice systemctl is-active machine-user.service", timeout=300)
            assert host.succeed("timeout 60 machine ssh alice getent passwd alice").strip().endswith(":/run/current-system/sw/bin/bash"), "missing shell did not fall back to bash"
            host.succeed("timeout 60 machine ssh alice grep -qx /nonexistent /persist/etc/machine/shell")
          '';

          meta.timeout = 900;
        }
      )
      {
        inherit system;
        pkgs = nixpkgs.legacyPackages.${system};
      };
}
