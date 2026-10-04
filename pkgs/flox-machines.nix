{ buildGo127Module, lib }:

buildGo127Module {
  pname = "flox-machines";
  version = "0.1.0";
  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      ../go.mod
      ../go.sum
      ../cmd
      (lib.fileset.maybeMissing ../internal)
    ];
  };
  vendorHash = "sha256-IgJJLpGh9y9w6K4osiqY+El0rqSbE/4aMmvlbJK/8lc=";
  subPackages = [ "cmd/front-door" ];
  env.CGO_ENABLED = 0;
  postInstall = ''
    mv $out/bin/front-door $out/bin/flox-machines-front-door
  '';
}
