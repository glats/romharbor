{
  self,
  nixpkgs,
  pkgs,
  system,
}:
let
  makeConfig =
    service:
    (nixpkgs.lib.nixosSystem {
      inherit system;
      modules = [
        self.nixosModules.default
        {
          boot.isContainer = true;
          system.stateVersion = "26.05";
          services.romharbor = service;
        }
      ];
    }).config;
  disabled = makeConfig { };
  enabled = makeConfig { enable = true; };
  custom = makeConfig {
    enable = true;
    address = "127.0.0.1:9090";
    libraryPath = "/srv/roms";
    cachePath = "/srv/cache";
    databasePath = "/srv/database/app.db";
    sources = [
      "archive.org"
      "lolroms"
    ];
    cacheTtl = "12h";
  };
  service = enabled.systemd.services.romharbor;
in
assert !(disabled.systemd.services ? romharbor);
assert builtins.all (item: item.assertion) enabled.assertions;
assert builtins.all (item: item.assertion) custom.assertions;
assert service.environment.HTTP_ADDR == "127.0.0.1:8080";
assert service.environment.DB_SEED_ENABLED == "false";
assert service.serviceConfig.ProtectSystem == "strict";
assert service.serviceConfig.User == "romharbor";
assert custom.systemd.services.romharbor.environment.ROM_LIBRARY_PATH == "/srv/roms";
assert custom.systemd.services.romharbor.environment.ROM_SOURCES == "archive.org,lolroms";
assert custom.systemd.services.romharbor.environment.ROM_CACHE_TTL == "12h";
pkgs.writeText "romharbor-module-check" "Module defaults, overrides and sandbox evaluated successfully.\n"
