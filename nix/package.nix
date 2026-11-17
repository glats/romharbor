{
  lib,
  buildGo125Module,
}:
buildGo125Module {
  pname = "romharbor";
  version = "0-unstable";
  src = lib.cleanSource ../.;
  vendorHash = "sha256-Xc/TBVIL23nR+qqGOI/1WxlhvHn8gj48nVDhQhIKwx8=";
  subPackages = [ "cmd/web" ];
  env.CGO_ENABLED = "1";

  checkPhase = ''
    runHook preCheck
    go test ./...
    runHook postCheck
  '';

  postInstall = ''
    mv "$out/bin/web" "$out/bin/romharbor"
  '';

  meta = {
    description = "Self-hosted ROM catalog and download service";
    homepage = "https://github.com/glats/romharbor";
    license = lib.licenses.mit;
    mainProgram = "romharbor";
    platforms = [
      "x86_64-linux"
      "aarch64-linux"
    ];
  };
}
