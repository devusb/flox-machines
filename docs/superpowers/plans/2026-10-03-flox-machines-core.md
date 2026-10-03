# Flox Machines Core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A NixOS host module that turns one declared guest template into imperatively created microVM instances, restarts them when the template changes, and keeps each person's home across restarts while the guest's writable store is rebuilt, all proven by NixOS tests.

**Architecture:** Two repositories. The microvm.nix fork gains an `instances` feature: a `microvm.templates` host option, a `microvm -c NAME -t TEMPLATE` create path, runner scripts that read per-instance values from `instance.env` at launch, and a guest module that takes identity from a shared directory. The platform repository provides the `floxMachines` NixOS module, the guest template, the `machine` CLI, and the NixOS tests that exercise creation, restart on base change, and store behavior across reboots.

**Tech Stack:** NixOS 26.11 (nixos-unstable), microvm.nix fork at `github:devusb/microvm.nix` branch `instances`, cloud-hypervisor, virtiofs, systemd-networkd, dnsmasq, NixOS test framework with nested KVM, home-manager, flox.

**Spec:** `docs/superpowers/specs/2026-10-03-flox-machines-design.md`

## Global Constraints

- System: `x86_64-linux` only. No other system in `checks` or `nixosConfigurations`.
- Every NixOS test is built on the remote builder: `nix build -L --max-jobs 0 --builders 'ssh-ng://mhelton@chopper x86_64-linux - 8 1 kvm,nixos-test,big-parallel' <installable>`. Never run a check locally on r2d2.
- Tailscale is out of scope for this plan. No tailscale package, service or test anywhere in it.
- Hypervisor for templates and tests: `cloud-hypervisor`. Other runners keep their build-time values and are not changed.
- Instance state directory: `/var/lib/microvms/<instance>`. Template runners: `/var/lib/microvms/.templates/<template>/current`.
- Instance names created by the platform CLI are prefixed `machine-`.
- Guest volumes: `home.img` at `/home`, `state.img` at `/var/lib/machine`, `store.img` at `/nix/.rw-store`. `store.img` is deleted before every boot.
- The guest template sets `nix.settings.auto-optimise-store = false` and `users.mutableUsers = true`.
- Commit messages follow the `writing-commits` skill: conventional type, terse subject, no generated-by footer.
- Platform work happens in a worktree under `.worktrees/core` on branch `core`. Fork work happens in `~/code/microvm.nix` on branch `instances`.
- Every design choice not in the spec gets an entry in `docs/decisions.md` in the same commit.

## Review Focus

1. A second instance created while the first is running must get a different tap name and MAC, or the bridge silently drops one guest's traffic. Pinned by Task 3's two-instance test.
2. A host switch that does not change the template must not restart any instance. Pinned by Task 5's no-op switch assertion.
3. A `nix build` inside the guest of a path that exists in the host's shared store but not in the guest database must succeed, or the first host package a person's build overlaps will break their build. Pinned by Task 8's shadow-path test.
4. The identity service must create the home directory after `/home` is mounted, or the person's files land on the root tmpfs and vanish. Pinned by Task 4's assertion that `/home/<user>` is on the home volume.
5. `machine destroy` on an instance that is still running must stop it first, or the service restarts a guest whose directory is gone. Pinned by Task 7's destroy-while-running test.

---

### Task 0: Repositories and worktrees

**Files:**
- Create: `~/code/microvm.nix` (clone of the fork), branch `instances`
- Create: `~/code/flox-machines/.worktrees/core`
- Modify: `~/code/flox-machines/.gitignore`

**Interfaces:**
- Produces: a platform worktree on branch `core`; a fork clone whose `instances` branch tracks `upstream/main` at commit `3f1540f`.

- [ ] **Step 1: Commit the platform docs on `main`**

Run from `~/code/flox-machines`:
```bash
printf '.worktrees/\nresult\n' > .gitignore
git add .gitignore docs spikes
git commit -m "docs: add design spec, decision log and spike 1 report"
```
Expected: one commit on `main`.

- [ ] **Step 1b: Push `main` to the private platform repo**

The repo `devusb/flox-machines` already exists as a private repository and `origin` points at it. Run: `git push -u origin main`
Expected: `main` visible at `github.com/devusb/flox-machines`.

- [ ] **Step 2: Create the platform worktree**

Run: `git worktree add .worktrees/core -b core`
Expected: `.worktrees/core` exists on branch `core`.

- [ ] **Step 3: Fork microvm.nix and clone it**

```bash
gh repo fork microvm-nix/microvm.nix --clone=false
git clone git@github.com:devusb/microvm.nix.git ~/code/microvm.nix
cd ~/code/microvm.nix
git remote add upstream https://github.com/microvm-nix/microvm.nix.git
git fetch upstream
git checkout -b instances upstream/main
```
Expected: `git log -1 --format=%h` prints `3f1540f`.

- [ ] **Step 4: Confirm the fork's existing checks evaluate**

Run from `~/code/microvm.nix`: `nix flake show --json . | jq '.checks."x86_64-linux" | keys | length'`
Expected: a positive integer.

---

### Task 1: `microvm.templates` option and template install service (fork)

**Files:**
- Modify: `~/code/microvm.nix/nixos-modules/host/options.nix` (after the `vms` option, around line 180)
- Modify: `~/code/microvm.nix/nixos-modules/host/default.nix` (the `systemd.services` fold, lines 106–190, and `tmpfiles`, lines 37–80)
- Create: `~/code/microvm.nix/checks/instances.nix`
- Modify: `~/code/microvm.nix/checks/default.nix` (register the new check next to `imperative-template`)

**Interfaces:**
- Produces: option `microvm.templates.<name>` with sub-options `config` (deferred NixOS module, like `vms.<name>.config`), `specialArgs`, `extraModules`, `restartIfChanged` (bool, default `true`), `autostart` (bool, default `true`). Service `install-microvm-template-<name>.service` (oneshot, `RemainAfterExit`, `wantedBy = ["microvms.target"]`, `restartTriggers = [ runner ]`). The `microvm.vms` evaluation helper is reused to evaluate a template's config.
- Produces: directory `/var/lib/microvms/.templates/<name>/` owned `microvm:kvm`, containing `current -> <runner>`.
- Produces: contract for instance directories: `/var/lib/microvms/<inst>/template` holds a template name; the service relinks `<inst>/current` to the template's runner for every such directory; if `restartIfChanged` and `<inst>/booted` exists and differs from the new runner, it runs `systemctl restart microvm@<inst>.service`; if `autostart` and `<inst>/booted` does not exist, it runs `systemctl start microvm@<inst>.service`.

- [ ] **Step 1: Write the failing test**

Create `checks/instances.nix` exporting `instances`, a `make-test-python` test with one node `host` that imports `self.nixosModules.host`, sets `boot.kernelModules = ["kvm"]`, `virtualisation.qemu.options = ["-cpu" "kvm64,+svm,+vmx"]`, `virtualisation.diskSize = 8192`, `virtualisation.memorySize = 4096`, and declares:

```nix
microvm.templates.tmpl.config = {
  microvm = { hypervisor = "cloud-hypervisor"; vcpu = 1; mem = 512;
    shares = [{ proto = "virtiofs"; tag = "ro-store"; source = "/nix/store"; mountPoint = "/nix/.ro-store"; socket = "ro-store.sock"; }]; };
  networking.hostName = "tmpl";
  system.stateVersion = lib.trivial.release;
};
specialisation.v2.configuration.microvm.templates.tmpl.config.environment.etc."base-version".text = "2";
```

Test script for this task:
```python
host.wait_for_unit("multi-user.target")
host.succeed("test -L /var/lib/microvms/.templates/tmpl/current")
host.succeed("mkdir -p /var/lib/microvms/pre && echo tmpl > /var/lib/microvms/pre/template && chown -R microvm:kvm /var/lib/microvms/pre")
host.succeed("systemctl restart install-microvm-template-tmpl.service")
host.succeed("test -L /var/lib/microvms/pre/current")
old = host.succeed("readlink /var/lib/microvms/.templates/tmpl/current").strip()
host.succeed("/run/current-system/specialisation/v2/bin/switch-to-configuration test")
new = host.succeed("readlink /var/lib/microvms/.templates/tmpl/current").strip()
assert old != new, "template runner did not change"
assert host.succeed("readlink /var/lib/microvms/pre/current").strip() == new
```

Register it in `checks/default.nix` the same way `imperative-template.nix` is registered.

- [ ] **Step 2: Run the test to verify it fails**

Run from `~/code/microvm.nix`: `nix build -L --max-jobs 0 --builders 'ssh-ng://mhelton@chopper x86_64-linux - 8 1 kvm,nixos-test,big-parallel' .#checks.x86_64-linux.instances`
Expected: evaluation error, `The option microvm.templates does not exist`.

- [ ] **Step 3: Add the `microvm.templates` option in `options.nix`**

Same submodule shape as `vms` minus `flake`, `updateFlake` and `evaluatedConfig`, plus `restartIfChanged` and `autostart` with the defaults above.

- [ ] **Step 4: Add the install service and tmpfiles entry in `default.nix`**

For each template: a tmpfiles `d` entry for `${stateDir}/.templates/${name}` (user `microvm`, group `kvm`, mode `0775`), and the oneshot service described in Interfaces. The script iterates `${stateDir}/*/template`, compares `$(cat template)` with the template name, and applies the relink, restart and start rules. `systemctl` calls use `${pkgs.systemd}/bin/systemctl`. The service must not fail when no instance directory exists.

- [ ] **Step 5: Run the test to verify it passes**

Run: the same command as Step 2.
Expected: build succeeds, test log ends with the script completing.

- [ ] **Step 6: Commit**

```bash
git add nixos-modules/host checks
git commit -m "feat(host): add microvm.templates with instance relink on rebuild"
```

---

### Task 2: `microvm -c NAME -t TEMPLATE` and instance listing (fork)

**Files:**
- Modify: `~/code/microvm.nix/pkgs/microvm-command.nix` (getopts at line 34, `create` at line 110, `update` at line 135, `list` at line 170)
- Modify: `~/code/microvm.nix/checks/instances.nix`
- Modify: `~/code/microvm.nix/doc/src/microvm-command.md`

**Interfaces:**
- Consumes: template directory layout from Task 1.
- Produces: flag `-t <template>`. `microvm -c NAME -t TEMPLATE` creates `${STATE_DIR}/NAME/` with `template` (the template name), `current` (symlink to `readlink ${STATE_DIR}/.templates/TEMPLATE/current`), `instance.env`, and `instance/` (empty directory, mode `0775`, group `kvm`), then creates the two gcroot links exactly as the flake create path does. `instance.env` contains, one per line, `MICROVM_HOSTNAME=NAME`, `MICROVM_TAP_0=mvm-<first 8 hex of sha256(NAME)>`, `MICROVM_MAC_0=02:<bytes 0..4 of sha256(NAME) as colon-separated hex pairs>`. Flags `-m <MB>` and `-v <n>` add `MICROVM_MEM=` and `MICROVM_VCPU=` lines; absent flags add no line.
- Produces: `microvm -u NAME` on a directory with a `template` file relinks `current` from the template and applies `-R` as for flake VMs. `microvm -l` prints `NAME: template <template>` and the same current/stale/outdated states as flake VMs.

- [ ] **Step 1: Extend the failing test**

Append to the test script:
```python
host.succeed("microvm -c inst1 -t tmpl")
host.succeed("test -f /var/lib/microvms/inst1/template && grep -qx tmpl /var/lib/microvms/inst1/template")
host.succeed("grep -q '^MICROVM_HOSTNAME=inst1$' /var/lib/microvms/inst1/instance.env")
host.succeed("grep -Eq '^MICROVM_TAP_0=mvm-[0-9a-f]{8}$' /var/lib/microvms/inst1/instance.env")
host.succeed("grep -Eq '^MICROVM_MAC_0=02(:[0-9a-f]{2}){5}$' /var/lib/microvms/inst1/instance.env")
host.succeed("test -d /var/lib/microvms/inst1/instance")
host.succeed("microvm -c inst2 -t tmpl -m 768 -v 2")
host.succeed("grep -q '^MICROVM_MEM=768$' /var/lib/microvms/inst2/instance.env")
host.succeed("grep -q '^MICROVM_VCPU=2$' /var/lib/microvms/inst2/instance.env")
tap1 = host.succeed("grep MICROVM_TAP_0 /var/lib/microvms/inst1/instance.env")
tap2 = host.succeed("grep MICROVM_TAP_0 /var/lib/microvms/inst2/instance.env")
assert tap1 != tap2
host.succeed("microvm -l | grep -q 'inst1: template tmpl'")
host.fail("microvm -c inst1 -t tmpl")
```

- [ ] **Step 2: Run the test to verify it fails**

Run: the Task 1 build command.
Expected: fails at `microvm -c inst1 -t tmpl` with the getopts help output.

- [ ] **Step 3: Implement the flag and actions in `microvm-command.nix`**

Add `t:`, `m:`, `v:` to getopts. In `create`, branch on `TEMPLATE` being set. Hashes come from `sha256sum` in coreutils, already on the script's `PATH` via `nix`'s closure; add `coreutils` to `makeBinPath` explicitly. Refuse to create if `${STATE_DIR}/NAME` exists.

- [ ] **Step 4: Run the test to verify it passes**

Expected: PASS.

- [ ] **Step 5: Document the flags**

Add a "Create an instance of a template" section to `doc/src/microvm-command.md` with the `-t`, `-m`, `-v` flags and the `instance.env` keys.

- [ ] **Step 6: Commit**

```bash
git add pkgs/microvm-command.nix checks doc
git commit -m "feat(cli): create instances from templates with instance.env"
```

---

### Task 3: Runner late binding for cloud-hypervisor (fork)

**Files:**
- Modify: `~/code/microvm.nix/lib/runner.nix` (`microvm-run` script, lines 174–186)
- Modify: `~/code/microvm.nix/lib/runners/cloud-hypervisor.nix` (`cpusOps` line 147–153, `memOps`, `--net` map lines 274–290)
- Modify: `~/code/microvm.nix/nixos-modules/microvm/interfaces.nix` (`tap-up`, `tap-down`, lines 23–38)
- Modify: `~/code/microvm.nix/nixos-modules/host/default.nix` (`microvm-tap-interfaces@`, line 191: add `WorkingDirectory = "${stateDir}/%i"`)
- Modify: `~/code/microvm.nix/checks/instances.nix`

**Interfaces:**
- Consumes: `instance.env` keys from Task 2.
- Produces: `microvm-run` begins by exporting `MICROVM_VCPU`, `MICROVM_MEM`, `MICROVM_HOSTNAME`, and for each interface index `i`, `MICROVM_TAP_i` and `MICROVM_MAC_i`, each set to the configured value, then runs `[ -f ./instance.env ] && set -a && . ./instance.env && set +a`. The cloud-hypervisor command uses `--cpus "boot=$MICROVM_VCPU"`, `--memory "size=${MICROVM_MEM}M,..."` and `--net "tap=$MICROVM_TAP_i,mac=$MICROVM_MAC_i"` for tap interfaces. `tap-up` and `tap-down` source `./instance.env` the same way and use `$MICROVM_TAP_i` with the configured id as default. Other runners are untouched.

- [ ] **Step 1: Write the failing assertions**

Append to the test script:
```python
runner = host.succeed("readlink /var/lib/microvms/.templates/tmpl/current").strip()
host.succeed(f"grep -q 'boot=$MICROVM_VCPU' {runner}/bin/microvm-run")
host.succeed(f"grep -q 'size=${{MICROVM_MEM}}M' {runner}/bin/microvm-run")
host.succeed("systemctl start microvm@inst1.service microvm@inst2.service")
host.wait_for_unit("microvm@inst1.service")
host.wait_for_unit("microvm@inst2.service")
tap1 = host.succeed("sed -n 's/^MICROVM_TAP_0=//p' /var/lib/microvms/inst1/instance.env").strip()
tap2 = host.succeed("sed -n 's/^MICROVM_TAP_0=//p' /var/lib/microvms/inst2/instance.env").strip()
host.succeed(f"ip link show {tap1}")
host.succeed(f"ip link show {tap2}")
host.succeed("pgrep -f 'cloud-hypervisor.*boot=2' >/dev/null")
```
Also add one tap interface to the template: `microvm.interfaces = [{ type = "tap"; id = "mvm-tmpl"; mac = "02:00:00:00:00:00"; }];`.

- [ ] **Step 2: Run the test to verify it fails**

Expected: the `grep` for `boot=$MICROVM_VCPU` fails.

- [ ] **Step 3: Implement late binding**

In `runner.nix`, prepend the exports and the `instance.env` sourcing to `microvm-run`. In `cloud-hypervisor.nix`, build the three affected arguments as unquoted shell strings so the variables expand at launch; every other argument stays under `lib.escapeShellArgs`. In `interfaces.nix`, source `instance.env` and reference `${MICROVM_TAP_i:-<id>}`. Add the `WorkingDirectory` to the tap unit.

- [ ] **Step 4: Run the test to verify it passes**

Expected: PASS, two guests running with distinct taps.

- [ ] **Step 5: Run the fork's existing cloud-hypervisor checks**

Run: `nix build -L --max-jobs 0 --builders '...' .#checks.x86_64-linux.vm-cloud-hypervisor-virtiofs` (use the exact attribute name `nix flake show` prints for the cloud-hypervisor virtiofs variant).
Expected: PASS. Declared VMs without `instance.env` behave as before.

- [ ] **Step 6: Commit**

```bash
git add lib nixos-modules checks
git commit -m "feat(runner): read per-instance cpu, memory and tap values at launch"
```

---

### Task 4: Guest `microvm.instance` module (fork)

**Files:**
- Create: `~/code/microvm.nix/nixos-modules/microvm/instance.nix`
- Modify: `~/code/microvm.nix/nixos-modules/microvm/default.nix` (import the new module next to `./vsock-ssh.nix`)
- Modify: `~/code/microvm.nix/checks/instances.nix`
- Modify: `~/code/microvm.nix/doc/src/declarative.md` (new section "Templates and instances")

**Interfaces:**
- Consumes: `instance/` directory from Task 2; `MICROVM_HOSTNAME` is not used by the guest, the guest reads files.
- Produces: option `microvm.instance.enable` (bool, default `false`) and `microvm.instance.user.shell` (package, default `pkgs.bash`). When enabled: a virtiofs share `{ tag = "instance"; source = "instance"; mountPoint = "/run/microvm/instance"; socket = "instance.sock"; }` is added, and service `microvm-instance-identity.service` (oneshot, `wantedBy = ["sysinit.target"]`, `after = ["run-microvm-instance.mount" "home.mount"]`, `before = ["network-pre.target"]`) sets the transient hostname from `/run/microvm/instance/hostname` when that file exists, and when `/run/microvm/instance/user` exists creates that user with `useradd --uid 1000 --create-home --home-dir /home/<user> --shell <shell> --user-group` if `getent passwd <user>` fails, then `chown <user>: /home/<user>`. The module asserts `users.mutableUsers`.
- Produces: file contract for `instance/`: `hostname`, `user`, both optional single-line files.

- [ ] **Step 1: Write the failing assertions**

Give the test host a bridge and DHCP so the script can reach guests: `systemd.network` bridge `vmbr0` with address `10.100.0.1/24`, a `.network` matching `Name=mvm-*` with `Bridge=vmbr0`, `services.dnsmasq` with `interface=vmbr0` and `dhcp-range=10.100.0.10,10.100.0.250,12h`. Template: `microvm.instance.enable = true; services.openssh.enable = true; services.openssh.settings.PermitRootLogin = "yes"; users.users.root.password = "test"; users.mutableUsers = true; networking.useDHCP = true; networking.usePredictableInterfaceNames = false;`. Test host gets `sshpass` and `openssh` in `environment.systemPackages`.

Append:
```python
host.succeed("echo inst1 > /var/lib/microvms/inst1/instance/hostname; echo alice > /var/lib/microvms/inst1/instance/user")
host.succeed("systemctl restart microvm@inst1.service")
host.wait_for_unit("microvm@inst1.service")
host.wait_until_succeeds("grep -q ' inst1 ' /var/lib/dnsmasq/dnsmasq.leases", timeout=120)
ip1 = host.succeed("awk '$4==\"inst1\"{print $3}' /var/lib/dnsmasq/dnsmasq.leases").strip()
ssh = f"sshpass -p test ssh -o StrictHostKeyChecking=no root@{ip1}"
host.wait_until_succeeds(f"{ssh} true", timeout=120)
assert host.succeed(f"{ssh} hostname").strip() == "inst1"
host.succeed(f"{ssh} id alice")
host.succeed(f"{ssh} findmnt -n -o SOURCE /home | grep -q /dev/vd")
host.succeed(f"{ssh} 'stat -c %U /home/alice' | grep -qx alice")
```
Template volumes for this: `[{ image = "home.img"; mountPoint = "/home"; size = 256; }]`.

- [ ] **Step 2: Run the test to verify it fails**

Expected: `The option microvm.instance does not exist`.

- [ ] **Step 3: Implement `instance.nix`**

As described in Interfaces. Use `hostnamectl --transient hostname` for the hostname. Use `pkgs.shadow` for `useradd`.

- [ ] **Step 4: Run the test to verify it passes**

Expected: PASS.

- [ ] **Step 5: Document templates and instances**

Add the section to `doc/src/declarative.md`: the `microvm.templates` option, the create command, the `instance.env` and `instance/` contracts, and what a host rebuild does to instances.

- [ ] **Step 6: Commit**

```bash
git add nixos-modules checks doc
git commit -m "feat(guest): take hostname and user from the instance share"
```

---

### Task 5: End-to-end restart on base change (fork)

**Files:**
- Modify: `~/code/microvm.nix/checks/instances.nix`

**Interfaces:**
- Consumes: everything above.
- Produces: the proven contract that a host switch restarts exactly the instances whose template changed, and leaves the home volume intact.

- [ ] **Step 1: Write the failing assertions**

Append:
```python
host.succeed(f"{ssh} 'echo keep > /home/alice/keep'")
t1_before = host.succeed("systemctl show -p ActiveEnterTimestampMonotonic microvm@inst1.service").strip()
t2_before = host.succeed("systemctl show -p ActiveEnterTimestampMonotonic microvm@inst2.service").strip()
host.succeed("/run/current-system/bin/switch-to-configuration test")
assert host.succeed("systemctl show -p ActiveEnterTimestampMonotonic microvm@inst1.service").strip() == t1_before, "no-op switch restarted inst1"
host.succeed("/run/current-system/specialisation/v2/bin/switch-to-configuration test")
host.wait_until_succeeds(f"[ \"$(systemctl show -p ActiveEnterTimestampMonotonic microvm@inst1.service)\" != '{t1_before}' ]", timeout=120)
host.wait_until_succeeds(f"[ \"$(systemctl show -p ActiveEnterTimestampMonotonic microvm@inst2.service)\" != '{t2_before}' ]", timeout=120)
host.wait_for_unit("microvm@inst1.service")
host.wait_until_succeeds(f"{ssh} true", timeout=180)
assert host.succeed(f"{ssh} cat /etc/base-version").strip() == "2"
assert host.succeed(f"{ssh} cat /home/alice/keep").strip() == "keep"
host.succeed("microvm -c inst3 -t tmpl")
host.succeed("test \"$(readlink /var/lib/microvms/inst3/current)\" = \"$(readlink /var/lib/microvms/.templates/tmpl/current)\"")
```
Note: after the specialisation switch, `ip1` may change; re-read it from the leases file before the ssh assertions.

- [ ] **Step 2: Run the test to verify it fails or passes**

Expected: PASS if Tasks 1–4 are correct. If the no-op switch restarts an instance, fix the install service so it only restarts when `readlink booted` differs from the new runner.

- [ ] **Step 3: Push the branch**

```bash
git push -u origin instances
```
Expected: branch visible at `github.com/devusb/microvm.nix/tree/instances`.

- [ ] **Step 4: Commit any fix and push**

```bash
git commit -am "test: cover restart on base change and no-op switch"
git push
```

---

### Task 6: Platform flake, host module and guest template

**Files:**
- Create: `.worktrees/core/flake.nix`
- Create: `.worktrees/core/modules/host.nix`
- Create: `.worktrees/core/modules/template.nix`
- Create: `.worktrees/core/checks/default.nix`
- Create: `.worktrees/core/checks/create-restart.nix`
- Modify: `.worktrees/core/docs/decisions.md`

**Interfaces:**
- Consumes: fork features from Tasks 1–5 via flake input `microvm = { url = "git+file:///home/mhelton/code/microvm.nix?ref=instances"; }` during development.
- Produces: flake inputs `nixpkgs` (nixos-unstable), `microvm`, `home-manager` (follows nixpkgs), `flox` (`github:flox/flox`). Outputs `nixosModules.floxMachines`, `nixosModules.machineTemplate`, `checks.x86_64-linux.*`.
- Produces: `floxMachines` options: `enable`; `template` (deferred module, default `self.nixosModules.machineTemplate`); `storage` (enum `["image" "zfs"]`, default `"image"`); `zfs.parentDataset` (str); `defaults.mem` (int, default 4096); `defaults.vcpu` (int, default 2); `defaults.homeSize` (int MB, default 20480); `bridge.name` (default `vmbr0`); `bridge.subnet` (default `10.100.0.0/24`). When enabled: imports the fork host module; sets `microvm.templates.machine.config = { imports = [ cfg.template ]; _module.args.floxMachines = cfg; }`; systemd-networkd bridge with the first address of the subnet; `.network` matching `mvm-*` into the bridge; `services.dnsmasq` serving DHCP on the bridge from `.10` to `.250`; `networking.nat` with the bridge as internal interface; an ed25519 keypair at `/var/lib/flox-machines/id_ed25519` generated by a oneshot if missing; the `machine` CLI from Task 7 in `environment.systemPackages`.
- Produces: `machineTemplate` guest module: `microvm.hypervisor = "cloud-hypervisor"`, `microvm.instance.enable = true`, `microvm.vcpu`/`mem` from `floxMachines.defaults`, one tap interface, virtiofs `ro-store` share, volumes `home.img` (`/home`, `defaults.homeSize`), `state.img` (`/var/lib/machine`, 1024), `store.img` (`/nix/.rw-store`, 8192), `microvm.writableStoreOverlay = "/nix/.rw-store"`, `microvm.preStart = "rm -f store.img"`, `nix.settings.auto-optimise-store = false`, `nix.settings.experimental-features = ["nix-command" "flakes"]`, `nix.registry.nixpkgs.flake = inputs.nixpkgs`, `nix.registry.home-manager.flake = inputs.home-manager`, `nix.nixPath = ["nixpkgs=flake:nixpkgs"]`, `environment.systemPackages = [ git tmux home-manager flox ]`, `services.openssh` with `PermitRootLogin = "prohibit-password"` and `authorizedKeysFiles = ["/run/microvm/instance/authorized_keys"]`, `networking.useDHCP = true`, `networking.usePredictableInterfaceNames = false`, `users.mutableUsers = true`, bash `interactiveShellInit` that execs `tmux new-session -A -s main` when `SSH_CONNECTION` is set and `TMUX` is empty, `environment.etc."machine/base-version".text` from `floxMachines.baseVersion` (str option, default `"1"`), `system.stateVersion = "26.11"`.

- [ ] **Step 1: Write the failing test**

`checks/create-restart.nix`: host node imports `self.nixosModules.floxMachines` with `floxMachines.enable = true`, the nested-KVM qemu options from Task 1, `virtualisation.diskSize = 16384`, `virtualisation.memorySize = 6144`, and `specialisation.v2.configuration.floxMachines.baseVersion = "2"`.

```python
host.wait_for_unit("multi-user.target")
host.succeed("test -L /var/lib/microvms/.templates/machine/current")
host.succeed("machine create alice")
host.succeed("machine create bob")
host.wait_for_unit("microvm@machine-alice.service")
host.wait_for_unit("microvm@machine-bob.service")
host.wait_until_succeeds("machine ssh alice true", timeout=180)
host.wait_until_succeeds("machine ssh bob true", timeout=180)
assert host.succeed("machine ssh alice hostname").strip() == "machine-alice"
host.succeed("machine ssh alice id alice")
host.succeed("machine ssh alice 'cat /etc/machine/base-version' | grep -qx 1")
host.succeed("machine ssh alice 'su - alice -c \"echo keep > ~/keep\"'")
ta = host.succeed("systemctl show -p ActiveEnterTimestampMonotonic microvm@machine-alice.service").strip()
host.succeed("/run/current-system/specialisation/v2/bin/switch-to-configuration test")
host.wait_until_succeeds(f"[ \"$(systemctl show -p ActiveEnterTimestampMonotonic microvm@machine-alice.service)\" != '{ta}' ]", timeout=120)
host.wait_until_succeeds("machine ssh alice true", timeout=180)
host.succeed("machine ssh alice 'cat /etc/machine/base-version' | grep -qx 2")
host.succeed("machine ssh alice 'cat /home/alice/keep' | grep -qx keep")
host.succeed("machine list | grep -q machine-alice")
```

- [ ] **Step 2: Run the test to verify it fails**

Run from `.worktrees/core`: `nix build -L --max-jobs 0 --builders 'ssh-ng://mhelton@chopper x86_64-linux - 8 1 kvm,nixos-test,big-parallel' .#checks.x86_64-linux.create-restart`
Expected: evaluation error for the missing flake or module.

- [ ] **Step 3: Write `flake.nix`, `modules/host.nix`, `modules/template.nix`**

Pass `inputs` to modules with `specialArgs`. If `inputs.flox` does not expose `packages.x86_64-linux.default`, use the attribute `nix flake show github:flox/flox` lists for the CLI and record the attribute in `docs/decisions.md`.

- [ ] **Step 4: Run the test to verify it fails only on the CLI**

Expected: fails at `machine create alice` with command not found. The template directory assertion passes.

- [ ] **Step 5: Record decisions**

Append to `docs/decisions.md`: admin access to guests in v1 is root SSH over the host-only bridge with a host-generated key rather than vsock; DHCP from dnsmasq on the bridge with hostnames in the leases file; guest base version exposed at `/etc/machine/base-version`.

- [ ] **Step 6: Commit**

```bash
git add flake.nix flake.lock modules checks docs
git commit -m "feat: add floxMachines host module and guest template"
```

---

### Task 7: `machine` CLI

**Files:**
- Create: `.worktrees/core/pkgs/machine-cli.nix`
- Modify: `.worktrees/core/modules/host.nix` (install the package, pass `storage`, `zfs.parentDataset`, `defaults`, bridge subnet)
- Modify: `.worktrees/core/checks/create-restart.nix`

**Interfaces:**
- Consumes: `microvm -c`, `instance/` and `instance.env` contracts from Tasks 2 and 4; the host key at `/var/lib/flox-machines/id_ed25519`.
- Produces: `machine create <name>`: runs `microvm -c machine-<name> -t machine -m <defaults.mem> -v <defaults.vcpu>`, writes `instance/hostname` = `machine-<name>`, `instance/user` = `<name>`, `instance/authorized_keys` = the host public key, then `systemctl start microvm@machine-<name>.service`. With `storage = "zfs"`: before starting, `zfs create -V <homeSize>M <parent>/<name>-home` and `-V 1024M <parent>/<name>-state`, `mkfs.ext4` on each `/dev/zvol/...` device, and symlinks `home.img` and `state.img` in the instance directory to those devices. `machine ssh <name> [cmd...]`: looks up the IP for hostname `machine-<name>` in `/var/lib/dnsmasq/dnsmasq.leases` and runs `ssh -i /var/lib/flox-machines/id_ed25519 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null root@<ip> cmd...`. `machine restart <name>`: `systemctl restart`. `machine resize <name> <mem-MB> <vcpu>`: replaces or adds the `MICROVM_MEM` and `MICROVM_VCPU` lines in `instance.env`, then restarts. `machine reimage <name>`: stop, `rm -f store.img`, start. `machine destroy <name>`: stop the service, remove the instance directory and gcroot links, and with `zfs` destroy both zvols. `machine list`: `microvm -l` filtered to `machine-` entries. Names are validated against `^[a-z][a-z0-9-]{0,30}$`.

- [ ] **Step 1: Extend the failing test**

Append:
```python
host.succeed("machine create carol")
host.wait_for_unit("microvm@machine-carol.service")
host.succeed("machine destroy carol")
host.fail("systemctl is-active microvm@machine-carol.service")
host.succeed("test ! -e /var/lib/microvms/machine-carol")
host.fail("machine create 'Bad Name'")
host.succeed("machine resize alice 1024 1")
host.succeed("grep -q '^MICROVM_MEM=1024$' /var/lib/microvms/machine-alice/instance.env")
host.wait_until_succeeds("machine ssh alice true", timeout=180)
host.succeed("pgrep -f 'cloud-hypervisor.*size=1024M' >/dev/null")
```

- [ ] **Step 2: Run the test to verify it fails**

Expected: fails at the first `machine create alice` in the Task 6 script, command not found.

- [ ] **Step 3: Implement `pkgs/machine-cli.nix`**

A `writeShellApplication` named `machine` with `runtimeInputs = [ microvmCommand openssh gawk coreutils systemd ]` plus `zfs` and `e2fsprogs` when the backend is `zfs`. Take backend, parent dataset, defaults and key path as function arguments from the host module.

- [ ] **Step 4: Run the full check to verify it passes**

Expected: PASS for the Task 6 and Task 7 scripts together.

- [ ] **Step 5: Commit**

```bash
git add pkgs modules checks
git commit -m "feat: add machine CLI for instance lifecycle"
```

---

### Task 8: Store behavior across reboots

**Files:**
- Create: `.worktrees/core/checks/store-reboot.nix`
- Modify: `.worktrees/core/checks/default.nix`
- Modify: `.worktrees/core/docs/decisions.md`

**Interfaces:**
- Consumes: `machine` CLI; template's ephemeral overlay.
- Produces: proven v1 store contract: paths a person adds vanish on restart, their home does not, the daemon works again after restart, and building a path that already exists in the shared host store succeeds.

- [ ] **Step 1: Write the failing test**

Host as in Task 6, plus `environment.systemPackages = [ pkgs.nix ]` already present. Script:
```python
host.wait_for_unit("multi-user.target")
host.succeed("machine create alice")
host.wait_until_succeeds("machine ssh alice true", timeout=180)
p = host.succeed("machine ssh alice 'su - alice -c \"echo hello > ~/f && nix store add-file ~/f\"'").strip()
host.succeed(f"machine ssh alice 'nix path-info {p}'")
host.succeed(f"machine ssh alice 'test -e {p}'")
host.succeed("machine restart alice")
host.wait_until_succeeds("machine ssh alice true", timeout=180)
host.fail(f"machine ssh alice 'nix path-info {p}'")
host.fail(f"machine ssh alice 'test -e {p}'")
host.succeed("machine ssh alice 'cat /home/alice/f' | grep -qx hello")
p2 = host.succeed("machine ssh alice 'su - alice -c \"nix store add-file ~/f\"'").strip()
assert p2 == p
bash = host.succeed("readlink -f $(which bash)").strip()
drv = f"derivation {{ name = \"fm-probe\"; system = \"x86_64-linux\"; builder = \"{bash}\"; args = [\"-c\" \"echo probe > $out\"]; }}"
out_host = host.succeed(f"nix build --no-link --print-out-paths --expr '{drv}'").strip()
out_guest = host.succeed(f"machine ssh alice 'su - alice -c \"nix build --no-link --print-out-paths --expr \\\"{drv}\\\"\"'").strip()
assert out_guest == out_host
host.succeed(f"machine ssh alice 'cat {out_host}' | grep -qx probe")
host.succeed("machine reimage alice")
host.wait_until_succeeds("machine ssh alice true", timeout=180)
host.succeed("machine ssh alice 'cat /home/alice/f' | grep -qx hello")
```
The guest template must allow `nix build` for an unprivileged user with no sandbox: add `nix.settings.sandbox = false` to the template for the test host only, via the check's own `floxMachines.template` override, and assert that `bash` resolves to a path present in the guest's shared store.

- [ ] **Step 2: Run the test**

Run: `nix build -L --max-jobs 0 --builders '...' .#checks.x86_64-linux.store-reboot`
Expected: PASS. If the shadow-path build fails, capture the nix error into `docs/decisions.md` under a new entry "Shadow paths in the shared store" and mark the assertion as a known failure with `host.fail`, so the test documents the behavior rather than hiding it.

- [ ] **Step 3: Record the contract**

Append to `docs/decisions.md`: the v1 store contract as proven, including the shadow-path result.

- [ ] **Step 4: Commit**

```bash
git add checks docs
git commit -m "test: pin store behavior across guest restarts"
```

---

### Task 9: ZFS storage backend

**Files:**
- Create: `.worktrees/core/checks/zfs-backend.nix`
- Modify: `.worktrees/core/modules/host.nix` (assert `zfs.parentDataset` set when `storage = "zfs"`; add `boot.supportedFilesystems = ["zfs"]`; enable `services.zfs.autoSnapshot` and set `com.sun:auto-snapshot=true` on the parent dataset in the create path so instance zvols are snapshotted daily)
- Modify: `.worktrees/core/pkgs/machine-cli.nix` (if Task 7 left any zfs path unimplemented)

**Interfaces:**
- Consumes: `machine` CLI zfs branch from Task 7.
- Produces: proven zvol lifecycle.

- [ ] **Step 1: Write the failing test**

Host as in Task 6 plus `virtualisation.emptyDiskImages = [ 8192 ]`, `networking.hostId = "8425e349"`, `floxMachines.storage = "zfs"`, `floxMachines.zfs.parentDataset = "tank/machines"`, `floxMachines.defaults.homeSize = 1024`. Script:
```python
host.wait_for_unit("multi-user.target")
host.succeed("zpool create tank /dev/vdb && zfs create tank/machines")
host.succeed("machine create alice")
host.succeed("zfs list -H -o name | grep -qx tank/machines/alice-home")
host.succeed("test -L /var/lib/microvms/machine-alice/home.img")
host.wait_until_succeeds("machine ssh alice true", timeout=180)
host.succeed("machine ssh alice 'findmnt -n -o SOURCE /home' | grep -q /dev/vd")
host.succeed("machine ssh alice 'su - alice -c \"echo z > ~/z\"'")
host.succeed("machine restart alice")
host.wait_until_succeeds("machine ssh alice true", timeout=180)
host.succeed("machine ssh alice 'cat /home/alice/z' | grep -qx z")
host.succeed("machine destroy alice")
host.fail("zfs list -H -o name | grep -qx tank/machines/alice-home")
```

- [ ] **Step 2: Run the test to verify it fails**

Expected: fails at `zfs create` or at the zvol assertion.

- [ ] **Step 3: Implement the zfs branch**

As in Task 7's Interfaces. `mkfs.ext4 -L home` and `-L state` on the zvol devices; wait for `/dev/zvol/<parent>/<name>-home` to appear with `udevadm settle`.

- [ ] **Step 4: Run the test to verify it passes**

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add checks modules pkgs
git commit -m "feat: add zfs storage backend for instance volumes"
```

---

### Task 10: Registry pin, home-manager and flox in the guest

**Files:**
- Modify: `.worktrees/core/checks/create-restart.nix`
- Modify: `.worktrees/core/modules/template.nix` (only if an assertion fails)
- Create: `.worktrees/core/README.md`

**Interfaces:**
- Consumes: template from Task 6.
- Produces: proof that the pinned registry resolves to the host flake's inputs and both tools run.

- [ ] **Step 1: Write the assertions**

Append to `create-restart.nix`:
```python
host.succeed("machine ssh alice 'nix registry list' | grep -q 'flake:nixpkgs path:'")
host.succeed("machine ssh alice 'nix registry list' | grep -q 'flake:home-manager'")
host.succeed("machine ssh alice 'home-manager --version'")
host.succeed("machine ssh alice 'flox --version'")
host.succeed("machine ssh alice 'tmux -V'")
```

- [ ] **Step 2: Run the check**

Expected: PASS. Fix the template if any assertion fails.

- [ ] **Step 3: Write `README.md`**

Sections: what Flox Machines is in three sentences; enabling the module on a host with a minimal `flake.nix` snippet; the `machine` commands; the storage backends; how to run the checks on chopper.

- [ ] **Step 4: Commit**

```bash
git add checks modules README.md
git commit -m "docs: add README and pin registry assertions"
```

---

### Task 11: Point the platform at the pushed fork

**Files:**
- Modify: `.worktrees/core/flake.nix`

- [ ] **Step 1: Switch the input**

Change `microvm.url` to `github:devusb/microvm.nix/instances` and run `nix flake update microvm`.

- [ ] **Step 2: Run all checks**

Run: `nix build -L --max-jobs 0 --builders '...' .#checks.x86_64-linux.create-restart .#checks.x86_64-linux.store-reboot .#checks.x86_64-linux.zfs-backend`
Expected: all PASS.

- [ ] **Step 3: Commit**

```bash
git add flake.nix flake.lock
git commit -m "chore: consume microvm.nix instances branch from the fork"
```
