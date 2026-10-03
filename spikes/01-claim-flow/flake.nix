{
  description = "Throwaway spike: microvm guest that joins the tailnet by claim link";

  inputs = {
    microvm.url = "github:microvm-nix/microvm.nix";
    nixpkgs.follows = "microvm/nixpkgs";
  };

  outputs = { self, nixpkgs, microvm }: {
    nixosConfigurations.claimspike = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [
        microvm.nixosModules.microvm
        ({ pkgs, ... }: {
          networking.hostName = "claimspike";
          boot.kernelParams = [ "loglevel=7" ];
          microvm.qemu.machine = "q35";
          system.stateVersion = "26.11";

          microvm = {
            hypervisor = "qemu";
            vcpu = 2;
            mem = 2048;
            interfaces = [{
              type = "user";
              id = "eth0";
              mac = "02:00:00:00:00:01";
            }];
            shares = [{
              source = "/nix/store";
              mountPoint = "/nix/.ro-store";
              tag = "ro-store";
              proto = "9p";
            }];
            volumes = [
              { image = "tailscale-state.img"; mountPoint = "/var/lib/tailscale"; size = 256; }
              { image = "home.img"; mountPoint = "/home"; size = 2048; }
            ];
          };

          networking.firewall.trustedInterfaces = [ "tailscale0" ];
          services.tailscale.enable = true;

          systemd.services.tailscale-claim = {
            description = "Bring up tailscale and surface the login URL on the console";
            after = [ "tailscaled.service" "network-online.target" ];
            wants = [ "network-online.target" ];
            wantedBy = [ "multi-user.target" ];
            path = [ pkgs.tailscale pkgs.jq ];
            serviceConfig.Type = "simple";
            script = ''
              tailscale up --ssh --hostname=claimspike --accept-dns=false --reset >/dev/console 2>&1 &
              for i in $(seq 1 60); do
                url=$(tailscale status --json 2>/dev/null | jq -r '.AuthURL // empty')
                state=$(tailscale status --json 2>/dev/null | jq -r '.BackendState // empty')
                if [ "$state" = "Running" ]; then echo "CLAIMSPIKE: tailnet joined as $(tailscale status --json | jq -r '.Self.DNSName')" >/dev/console; exit 0; fi
                if [ -n "$url" ]; then echo "CLAIMSPIKE LOGIN URL: $url" >/dev/console; echo "$url" > /var/lib/tailscale/login-url; fi
                sleep 2
              done
              wait
            '';
          };

          users.users.mhelton = {
            isNormalUser = true;
            shell = pkgs.bash;
          };
          programs.tmux.enable = true;
          programs.bash.interactiveShellInit = ''
            if [ -z "$TMUX" ] && [ -n "$SSH_CONNECTION" ]; then exec tmux new-session -A -s main; fi
          '';
          environment.systemPackages = with pkgs; [ git tmux jq ];
        })
      ];
    };
  };
}
