{ self, pkgs }:
pkgs.testers.runNixOSTest {
  name = "romharbor-service";
  nodes.machine = {
    imports = [ self.nixosModules.default ];
    services.romharbor.enable = true;
    environment.systemPackages = [ pkgs.curl ];
    virtualisation.memorySize = 1536;
    virtualisation.cores = 2;
    system.stateVersion = "26.05";
  };

  testScript = ''
    start_all()
    machine.wait_for_unit("romharbor.service")
    machine.wait_for_unit("romharbor-worker.service")
    machine.wait_for_open_port(8080)
    machine.succeed("curl -fsS http://127.0.0.1:8080/users")
    machine.succeed("test $(systemctl show -p User --value romharbor.service) = romharbor")
    machine.succeed("test $(systemctl show -p ProtectSystem --value romharbor.service) = strict")
    machine.succeed("test $(systemctl show -p User --value romharbor-worker.service) = romharbor")
    machine.succeed("test $(systemctl show -p ProtectSystem --value romharbor-worker.service) = strict")
    machine.succeed("echo preserved > /var/lib/romharbor/outside.json")
    machine.succeed("test $(curl -sS -o /tmp/rejected.json -w '%{http_code}' 'http://127.0.0.1:8080/api/games?platform=..%2Foutside&q=mario') = 400")
    machine.succeed("grep -F 'unknown platform' /tmp/rejected.json")
    machine.succeed("test $(cat /var/lib/romharbor/outside.json) = preserved")
    machine.succeed("curl -fsS --data 'name=ModuleSmoke&email=module@example.invalid' http://127.0.0.1:8080/users | grep ModuleSmoke")
    machine.succeed("test -s /var/lib/romharbor/app.db")
    machine.succeed("systemctl restart romharbor.service")
    machine.wait_for_unit("romharbor.service")
    machine.wait_for_open_port(8080)
    machine.succeed("curl -fsS http://127.0.0.1:8080/users | grep ModuleSmoke")
    machine.succeed("${
      self.packages.${pkgs.stdenv.hostPlatform.system}.romharbor
    }/bin/romharbor --help")
    machine.succeed("${
      self.packages.${pkgs.stdenv.hostPlatform.system}.romharbor
    }/bin/romharbor-worker --help")
  '';
}
