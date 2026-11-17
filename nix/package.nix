{
  lib,
  buildGo125Module,
}:
buildGo125Module {
  pname = "romharbor";
  version = "0-unstable";
  src = lib.cleanSource ../.;
  vendorHash = "sha256-KttuIe62ZK5y5A5qst3ZTalKtiRhNdhRRt1m4686CLE=";
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
