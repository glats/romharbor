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
  worker = enabled.systemd.services.romharbor-worker;
in
assert !(disabled.systemd.services ? romharbor);
assert !(disabled.systemd.services ? romharbor-worker);
assert builtins.all (item: item.assertion) enabled.assertions;
assert builtins.all (item: item.assertion) custom.assertions;
assert service.environment.HTTP_ADDR == "127.0.0.1:8080";
assert service.environment.DB_SEED_ENABLED == "false";
assert service.serviceConfig.ProtectSystem == "strict";
assert service.serviceConfig.User == "romharbor";
assert custom.systemd.services.romharbor.environment.ROM_LIBRARY_PATH == "/srv/roms";
assert custom.systemd.services.romharbor.environment.ROM_SOURCES == "archive.org,lolroms";
assert custom.systemd.services.romharbor.environment.ROM_CACHE_TTL == "12h";
assert
  worker.serviceConfig.ExecStart == "${self.packages.${system}.romharbor}/bin/romharbor-worker";
assert worker.serviceConfig.User == "romharbor";
assert worker.serviceConfig.Group == "romharbor";
assert worker.serviceConfig.StateDirectory == "romharbor";
assert worker.serviceConfig.ProtectSystem == "strict";
assert worker.serviceConfig.TimeoutStopSec == service.serviceConfig.TimeoutStopSec;
assert worker.environment.DATABASE_PATH == "/var/lib/romharbor/app.db";
assert worker.environment.ROM_LIBRARY_PATH == "/var/lib/romharbor/roms";
assert worker.environment.DB_SEED_ENABLED == "false";
assert worker.environment.LOG_PRETTY == "false";
assert !(worker.environment ? HTTP_ADDR);
assert !(worker.environment ? ROM_SOURCES);
assert !(builtins.elem "romharbor.service" worker.after);
assert !(builtins.elem "romharbor-worker.service" service.after);
assert !(builtins.elem "romharbor-worker.service" service.wantedBy);
assert custom.systemd.services.romharbor-worker.environment.ROM_LIBRARY_PATH == "/srv/roms";
assert custom.systemd.services.romharbor-worker.environment.DATABASE_PATH == "/srv/database/app.db";
pkgs.writeText "romharbor-module-check" "Module defaults, overrides and sandbox evaluated successfully.\n"
