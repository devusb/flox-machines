{
  lib,
  writeShellApplication,
  coreutils,
  gawk,
  gnugrep,
  gnused,
  openssh,
  systemd,
  e2fsprogs,
  zfs,
  storage,
  parentDataset,
  homeSize,
  keyDir,
  stateDir ? "/var/lib/microvms",
}:

writeShellApplication {
  name = "machine";
  runtimeInputs = [
    coreutils
    gawk
    gnugrep
    gnused
    openssh
    systemd
  ] ++ lib.optionals (storage == "zfs") [
    e2fsprogs
    zfs
  ];
  text = ''
    STATE_DIR=${stateDir}
    STORAGE=${storage}
    PARENT=${lib.escapeShellArg (toString parentDataset)}
    HOME_SIZE=${toString homeSize}
    KEY=${keyDir}/id_ed25519

    usage() {
      cat <<USAGE
    Usage: machine <command> [args]

      create <name>                 create and start a machine
      ssh <name> [command...]       run a command as root on a machine
      restart <name>                restart a machine
      resize <name> <mem-MB> <vcpu> set a per-machine size and restart
      resize <name> --reset         return to the template's size and restart
      reimage <name>                wipe the machine's Nix store layer and restart
      destroy <name>                stop and delete a machine and its volumes
      list                          list machines
      gc                            stop all machines, collect host garbage, start them
    USAGE
    }

    die() {
      echo "machine: $*" >&2
      exit 1
    }

    check_name() {
      [[ "$1" =~ ^[a-z][a-z0-9-]{0,30}$ ]] || die "invalid name '$1'"
    }

    instance() {
      echo "machine-$1"
    }

    dir() {
      echo "$STATE_DIR/$(instance "$1")"
    }

    require() {
      check_name "$1"
      [ -d "$(dir "$1")" ] || die "no machine '$1'"
    }

    unit() {
      echo "microvm@$(instance "$1").service"
    }

    zvol() {
      echo "$PARENT/$1-$2"
    }

    create_zvol() {
      local name=$1 volume=$2 size=$3 label=$4
      zfs create -o com.sun:auto-snapshot=true -V "''${size}M" "$(zvol "$name" "$volume")"
      udevadm settle
      mkfs.ext4 -q -L "$label" "/dev/zvol/$(zvol "$name" "$volume")"
      chown microvm:kvm "$(readlink -f "/dev/zvol/$(zvol "$name" "$volume")")"
      ln -s "/dev/zvol/$(zvol "$name" "$volume")" "$(dir "$name")/$volume.img"
    }

    cmd_create() {
      local name=$1
      check_name "$name"
      [ -e "$(dir "$name")" ] && die "machine '$name' exists"
      [ -f "$KEY.pub" ] || die "admin key $KEY.pub missing"

      microvm -c "$(instance "$name")" -t machine > /dev/null
      local d
      d=$(dir "$name")
      instance "$name" > "$d/instance/hostname"
      echo "$name" > "$d/instance/user"
      cp "$KEY.pub" "$d/instance/authorized_keys"

      if [ "$STORAGE" = zfs ]; then
        create_zvol "$name" home "$HOME_SIZE" home
        create_zvol "$name" state 1024 state
      fi

      chown -R microvm:kvm "$d"
      systemctl start "$(unit "$name")"
      echo "created $(instance "$name")"
    }

    address() {
      awk -v host="$(instance "$1")" '$4 == host { ip = $3 } END { print ip }' /var/lib/dnsmasq/dnsmasq.leases
    }

    cmd_ssh() {
      local name=$1
      shift
      require "$name"
      local ip
      ip=$(address "$name")
      [ -n "$ip" ] || die "machine '$name' has no address yet"
      exec ssh -i "$KEY" -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR "root@$ip" "$@"
    }

    cmd_restart() {
      require "$1"
      systemctl restart "$(unit "$1")"
    }

    cmd_resize() {
      local name=$1
      require "$name"
      local env
      env="$(dir "$name")/instance.env"
      sed -i '/^MICROVM_MEM=/d; /^MICROVM_VCPU=/d' "$env"
      if [ "''${2:-}" != --reset ]; then
        [[ "''${2:-}" =~ ^[0-9]+$ && "''${3:-}" =~ ^[0-9]+$ ]] || die "usage: machine resize <name> <mem-MB> <vcpu>"
        echo "MICROVM_MEM=$2" >> "$env"
        echo "MICROVM_VCPU=$3" >> "$env"
      fi
      systemctl restart "$(unit "$name")"
    }

    cmd_reimage() {
      require "$1"
      local d
      d=$(dir "$1")
      systemctl stop "$(unit "$1")"
      rm -f "$d/nix-store-overlay.img" "$d/nix-var.img"
      systemctl start "$(unit "$1")"
    }

    cmd_destroy() {
      require "$1"
      local name=$1 d
      d=$(dir "$name")
      systemctl stop "$(unit "$name")"
      rm -rf "$d"
      rm -f "/nix/var/nix/gcroots/microvm/$(instance "$name")" "/nix/var/nix/gcroots/microvm/booted-$(instance "$name")"
      if [ "$STORAGE" = zfs ]; then
        zfs destroy "$(zvol "$name" home)"
        zfs destroy "$(zvol "$name" state)"
      fi
      echo "destroyed $(instance "$name")"
    }

    cmd_list() {
      microvm -l | sed 's/\x1b\[[0-9;]*m//g' | grep '^machine-' || true
    }

    cmd_gc() {
      local running=()
      for d in "$STATE_DIR"/machine-*; do
        [ -d "$d" ] || continue
        local u
        u="microvm@$(basename "$d").service"
        if systemctl is-active -q "$u"; then
          running+=("$u")
        fi
      done
      if [ ''${#running[@]} -gt 0 ]; then
        systemctl stop "''${running[@]}"
      fi
      nix-collect-garbage
      if [ ''${#running[@]} -gt 0 ]; then
        systemctl start "''${running[@]}"
      fi
    }

    [ $# -ge 1 ] || { usage; exit 1; }
    command=$1
    shift
    case "$command" in
      create) [ $# -eq 1 ] || die "usage: machine create <name>"; cmd_create "$1" ;;
      ssh) [ $# -ge 1 ] || die "usage: machine ssh <name> [command...]"; cmd_ssh "$@" ;;
      restart) [ $# -eq 1 ] || die "usage: machine restart <name>"; cmd_restart "$1" ;;
      resize) [ $# -ge 2 ] || die "usage: machine resize <name> <mem-MB> <vcpu>"; cmd_resize "$@" ;;
      reimage) [ $# -eq 1 ] || die "usage: machine reimage <name>"; cmd_reimage "$1" ;;
      destroy) [ $# -eq 1 ] || die "usage: machine destroy <name>"; cmd_destroy "$1" ;;
      list) cmd_list ;;
      gc) cmd_gc ;;
      *) usage; exit 1 ;;
    esac
  '';
}
