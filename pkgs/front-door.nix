{ buildGo127Module, lib }:

buildGo127Module {
  pname = "flox-machines-front-door";
  version = "0.1.0";
  src = lib.cleanSource ../front-door;
  vendorHash = "sha256-IgJJLpGh9y9w6K4osiqY+El0rqSbE/4aMmvlbJK/8lc=";
  subPackages = [ "." ];
  env.CGO_ENABLED = 0;
  postInstall = ''
    mv $out/bin/front-door $out/bin/flox-machines-front-door
  '';
  meta.mainProgram = "flox-machines-front-door";
}
