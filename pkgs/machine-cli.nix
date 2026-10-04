{
  lib,
  writeShellApplication,
  coreutils,
  gawk,
  gnugrep,
  gnused,
  getent,
  jq,
  openssh,
  systemd,
  e2fsprogs,
  zfs,
  storage,
  parentDataset,
  persistSize,
  keyDir,
  stateDir ? "/var/lib/microvms",
  tailscaleStatusFilter ? ./tailscale-status.jq,
  reservedNames ? [ ],
}:

writeShellApplication {
  name = "machine";
  runtimeInputs = [
    coreutils
    gawk
    gnugrep
    gnused
    getent
    jq
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
    PERSIST_SIZE=${toString persistSize}
    KEY=${keyDir}/id_ed25519
    RESERVED=(${lib.escapeShellArgs ([ "admin" "root" "nobody" "sshd" "tailscale" "microvm" "nixbld" ] ++ reservedNames)})

    usage() {
      cat <<USAGE
    Usage: machine <command> [args]

      create <name> [--owner <login>]  create and start a machine
      status <name> --json          report a machine's state as JSON
      login <name>                  start a Tailscale login on a machine
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

    valid_name() {
      [[ "$1" =~ ^[a-z][a-z0-9-]{0,30}$ ]] || die "invalid name '$1'"
    }

    check_name() {
      valid_name "$1"
      local reserved
      for reserved in "''${RESERVED[@]}"; do
        [ "$1" = "$reserved" ] && die "reserved name '$1'"
      done
      local uid
      uid=$(getent passwd "$1" | cut -d: -f3) || true
      if [ -n "$uid" ] && [ "$uid" -lt 1000 ]; then
        die "reserved name '$1'"
      fi
    }

    instance() {
      echo "machine-$1"
    }

    dir() {
      echo "$STATE_DIR/$(instance "$1")"
    }

    require() {
      valid_name "$1"
      [ -d "$(dir "$1")" ] || die "no machine '$1'"
    }

    unit() {
      echo "microvm@$(instance "$1").service"
    }

    zvol() {
      echo "$PARENT/$1"
    }

    create_zvol() {
      local name=$1 size=$2
      zfs create -o com.sun:auto-snapshot=true -V "''${size}M" "$(zvol "$name")"
      udevadm settle
      mkfs.ext4 -q -L persist "/dev/zvol/$(zvol "$name")"
      chown microvm:kvm "$(readlink -f "/dev/zvol/$(zvol "$name")")"
      ln -s "/dev/zvol/$(zvol "$name")" "$(dir "$name")/persist.img"
    }

    cmd_create() {
      local name=$1 owner=""
      shift
      while [ $# -gt 0 ]; do
        case "$1" in
          --owner) [ $# -ge 2 ] || die "usage: machine create <name> [--owner <login>]"; owner=$2; shift 2 ;;
          *) die "usage: machine create <name> [--owner <login>]" ;;
        esac
      done
      check_name "$name"
      [ -e "$(dir "$name")" ] && die "machine '$name' exists"
      [ -f "$KEY.pub" ] || die "admin key $KEY.pub missing"

      microvm -c "$(instance "$name")" -t machine > /dev/null
      local d
      d=$(dir "$name")
      instance "$name" > "$d/instance/hostname"
      echo "$name" > "$d/instance/user"
      cp "$KEY.pub" "$d/instance/authorized_keys"
      readlink "$d/current/share/microvm/system" > "$d/instance/system"

      if [ "$STORAGE" = zfs ]; then
        create_zvol "$name" "$PERSIST_SIZE"
      fi

      chown -R microvm:kvm "$d"
      if [ -n "$owner" ]; then
        printf '%s\n' "$owner" > "$d/owner"
        chown root:root "$d/owner"
        chmod 600 "$d/owner"
      fi
      systemctl start "$(unit "$name")"
      echo "created $(instance "$name")"
    }

    address() {
      local mac
      mac=$(sed -n 's/^MICROVM_MAC_0=//p' "$(dir "$1")/instance.env")
      [ -n "$mac" ] || return 0
      awk -v mac="''${mac,,}" 'tolower($2) == mac { ip = $3 } END { print ip }' /var/lib/dnsmasq/dnsmasq.leases
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


    run_ssh() {
      local seconds=$1 name=$2 ip
      shift 2
      ip=$(address "$name")
      [ -n "$ip" ] || return 255
      timeout "$seconds" ssh -i "$KEY" -o BatchMode=yes -o ConnectTimeout=5 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR "root@$ip" "$@"
    }

    cmd_status() {
      local name=$1
      [ "''${2:-}" = --json ] || die "usage: machine status <name> --json"
      valid_name "$name"
      local d
      d=$(dir "$name")
      if [ ! -d "$d" ]; then
        jq -n -c --arg name "$name" '{name: $name, exists: false}'
        return
      fi
      local owner="" running=false reachable=false tailscale=null
      [ -f "$d/owner" ] && owner=$(cat "$d/owner")
      systemctl is-active -q "$(unit "$name")" && running=true
      if run_ssh 5 "$name" true; then
        reachable=true
        local raw
        raw=$(run_ssh 10 "$name" tailscale status --json 2> /dev/null) || true
        if [ -n "$raw" ]; then
          tailscale=$(jq -c -f ${tailscaleStatusFilter} <<< "$raw") || tailscale=null
        fi
      fi
      jq -n -c --arg name "$name" --arg owner "$owner" \
        --argjson running "$running" --argjson reachable "$reachable" --argjson tailscale "$tailscale" \
        '{name: $name, exists: true, owner: $owner, running: $running, reachable: $reachable}
         + (if $tailscale == null then {} else {tailscale: $tailscale} end)'
    }

    cmd_login() {
      require "$1"
      local name=$1 state
      local raw
      raw=$(run_ssh 10 "$name" tailscale status --json 2> /dev/null) || true
      state=$(jq -r '.BackendState // ""' <<< "''${raw:-null}") || state=""
      [ "$state" = Running ] && return 0
      run_ssh 15 "$name" 'systemctl stop machine-tailscale-login.service 2> /dev/null; systemd-run --quiet --collect --unit=machine-tailscale-login tailscale up --ssh' \
        || die "could not start a Tailscale login on machine '$name'"
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
      systemctl kill --signal=SIGKILL "$(unit "$name")" 2> /dev/null || true
      systemctl stop "$(unit "$name")"
      rm -rf "$d"
      rm -f "/nix/var/nix/gcroots/microvm/$(instance "$name")" "/nix/var/nix/gcroots/microvm/booted-$(instance "$name")"
      if [ "$STORAGE" = zfs ]; then
        udevadm settle
        for _ in $(seq 1 20); do
          zfs destroy -r "$(zvol "$name")" 2> /dev/null && break
          sleep 1
        done
        if zfs list "$(zvol "$name")" > /dev/null 2>&1; then
          die "could not destroy $(zvol "$name")"
        fi
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
      create) [ $# -ge 1 ] || die "usage: machine create <name> [--owner <login>]"; cmd_create "$@" ;;
      status) [ $# -ge 1 ] || die "usage: machine status <name> --json"; cmd_status "$@" ;;
      login) [ $# -eq 1 ] || die "usage: machine login <name>"; cmd_login "$1" ;;
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
