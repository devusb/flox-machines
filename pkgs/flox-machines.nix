{
  buildGo127Module,
  lib,
  makeWrapper,
  openssh,
  e2fsprogs,
  util-linux,
  coreutils,
}:

buildGo127Module {
  pname = "flox-machines";
  version = "0.1.0";
  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      ../go.mod
      ../go.sum
      ../cmd
      ../internal
    ];
  };
  vendorHash = "sha256-IgJJLpGh9y9w6K4osiqY+El0rqSbE/4aMmvlbJK/8lc=";
  subPackages = [
    "cmd/front-door"
    "cmd/machine"
  ];
  env.CGO_ENABLED = 0;
  nativeBuildInputs = [ makeWrapper ];
  postInstall = ''
    mv $out/bin/front-door $out/bin/flox-machines-front-door
    for bin in machine flox-machines-front-door; do
      wrapProgram $out/bin/$bin \
        --prefix PATH : ${
          lib.makeBinPath [
            openssh
            e2fsprogs
            util-linux
            coreutils
          ]
        } \
        --suffix PATH : /run/current-system/sw/bin
    done
  '';
  meta.mainProgram = "machine";
}
