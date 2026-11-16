{
  lib,
  buildGo125Module,
  makeWrapper,
  p7zip,
}:
buildGo125Module {
  pname = "romharbor";
  version = "0-unstable";
  src = lib.cleanSource ../.;
  vendorHash = "sha256-6FdeMEgi/0nxKhb/OMql4CW5yxihpyz6COztXOtSAy8=";
  subPackages = [ "cmd/web" ];
  env.CGO_ENABLED = "1";
  nativeBuildInputs = [ makeWrapper ];

  checkPhase = ''
    runHook preCheck
    go test ./...
    runHook postCheck
  '';

  postInstall = ''
    mv "$out/bin/web" "$out/bin/romharbor"
    wrapProgram "$out/bin/romharbor" --prefix PATH : ${lib.makeBinPath [ p7zip ]}
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
