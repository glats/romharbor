{
  description = "ROMHarbor catalog and download service";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
    in
    {
      packages = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        rec {
          romharbor = pkgs.callPackage ./nix/package.nix { };
          default = romharbor;
        }
      );

      apps = forAllSystems (system: {
        default = {
          type = "app";
          program = "${self.packages.${system}.romharbor}/bin/romharbor";
          meta.description = "ROMHarbor catalog server";
        };
      });

      nixosModules = rec {
        romharbor = import ./nix/module.nix { inherit self; };
        default = romharbor;
      };

      checks = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          package = self.packages.${system}.romharbor;
          module = import ./nix/checks.nix {
            inherit
              self
              nixpkgs
              pkgs
              system
              ;
          };
        }
        // nixpkgs.lib.optionalAttrs (system == "x86_64-linux") {
          service = import ./nix/service-test.nix { inherit self pkgs; };
        }
      );

      formatter = forAllSystems (system: nixpkgs.legacyPackages.${system}.nixfmt);
    };
}
