{ self }:
{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.services.romharbor;
  inherit (lib)
    mkEnableOption
    mkIf
    mkOption
    types
    ;
in
{
  options.services.romharbor = {
    enable = mkEnableOption "ROMHarbor";
    package = mkOption {
      type = types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.romharbor;
      description = "ROMHarbor package to run.";
    };
    address = mkOption {
      type = types.str;
      default = "127.0.0.1:8080";
      description = "HTTP listen address; expose through an authenticated reverse proxy.";
    };
    libraryPath = mkOption {
      type = types.str;
      default = "/var/lib/romharbor/roms";
      description = "Absolute writable ROM library directory.";
    };
    cachePath = mkOption {
      type = types.str;
      default = "/var/lib/romharbor/cache";
      description = "Absolute writable catalog cache directory.";
    };
    databasePath = mkOption {
      type = types.str;
      default = "/var/lib/romharbor/app.db";
      description = "Absolute SQLite database filename.";
    };
    sources = mkOption {
      type = types.listOf (
        types.enum [
          "archive.org"
          "dlarchive"
          "lolroms"
        ]
      );
      default = [ "archive.org" ];
      description = "Enabled catalog sources; supplemental sources are opt-in.";
    };
    cacheTtl = mkOption {
      type = types.str;
      default = "24h";
      description = "Listing lifetime as a Go duration.";
    };
    user = mkOption {
      type = types.str;
      default = "romharbor";
      description = "Existing service user, or the automatically created romharbor user.";
    };
    group = mkOption {
      type = types.str;
      default = "romharbor";
      description = "Existing service group, or the automatically created romharbor group.";
    };
  };

  config = mkIf cfg.enable {
    assertions = [
      {
        assertion = builtins.all (path: lib.hasPrefix "/" path) [
          cfg.libraryPath
          cfg.cachePath
          cfg.databasePath
        ];
        message = "ROMHarbor storage paths must be absolute.";
      }
      {
        assertion = cfg.sources != [ ];
        message = "ROMHarbor requires at least one source.";
      }
    ];

    users.users = mkIf (cfg.user == "romharbor") {
      romharbor = {
        isSystemUser = true;
        group = cfg.group;
      };
    };
    users.groups = mkIf (cfg.group == "romharbor") { romharbor = { }; };

    systemd.services.romharbor = {
      description = "ROMHarbor catalog and downloads";
      wantedBy = [ "multi-user.target" ];
      after = [ "network.target" ];
      environment = {
        HTTP_ADDR = cfg.address;
        DATABASE_PATH = cfg.databasePath;
        ROM_LIBRARY_PATH = cfg.libraryPath;
        ROM_CACHE_PATH = cfg.cachePath;
        ROM_SOURCES = lib.concatStringsSep "," cfg.sources;
        ROM_CACHE_TTL = cfg.cacheTtl;
        LOG_PRETTY = "false";
        DB_SEED_ENABLED = "false";
      };
      serviceConfig = {
        ExecStart = "${cfg.package}/bin/romharbor";
        User = cfg.user;
        Group = cfg.group;
        WorkingDirectory = "/var/lib/romharbor";
        StateDirectory = "romharbor";
        StateDirectoryMode = "0750";
        UMask = "0027";
        Restart = "on-failure";
        RestartSec = 5;
        TimeoutStopSec = 35;
        PrivateTmp = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        NoNewPrivileges = true;
        RestrictSUIDSGID = true;
        LockPersonality = true;
        ReadWritePaths = [
          "-${cfg.libraryPath}"
          "-${cfg.cachePath}"
          "-${builtins.dirOf cfg.databasePath}"
        ];
      };
    };

    systemd.services.romharbor-worker = {
      description = "ROMHarbor download worker";
      wantedBy = [ "multi-user.target" ];
      after = [ "network.target" ];
      environment = {
        DATABASE_PATH = cfg.databasePath;
        ROM_LIBRARY_PATH = cfg.libraryPath;
        LOG_PRETTY = "false";
        DB_SEED_ENABLED = "false";
      };
      serviceConfig = {
        ExecStart = "${cfg.package}/bin/romharbor-worker";
        User = cfg.user;
        Group = cfg.group;
        WorkingDirectory = "/var/lib/romharbor";
        StateDirectory = "romharbor";
        StateDirectoryMode = "0750";
        UMask = "0027";
        Restart = "on-failure";
        RestartSec = 5;
        TimeoutStopSec = 35;
        PrivateTmp = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        NoNewPrivileges = true;
        RestrictSUIDSGID = true;
        LockPersonality = true;
        ReadWritePaths = [
          "-${cfg.libraryPath}"
          "-${cfg.cachePath}"
          "-${builtins.dirOf cfg.databasePath}"
        ];
      };
    };
  };
}
