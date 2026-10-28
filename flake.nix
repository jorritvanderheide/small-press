{
  description = "small-press: a site from a folder of Markdown, with posts from In the Open";

  inputs.nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      # The example site, as a site's own flake has it (docs/sites.md): its
      # program, and the site it makes from example/content/.
      packages = forAllSystems (
        pkgs:
        let
          program = pkgs.callPackage ./nix/program.nix { };
        in
        {
          inherit program;
          site = pkgs.callPackage ./nix/site.nix {
            inherit program;
            # A working copy with changes has no count: no build number then.
            build = self.revCount or 0;
          };
          default = self.packages.${pkgs.stdenv.hostPlatform.system}.site;
        }
      );

      # services.small-press: every site on a host, each from its own
      # repository.
      nixosModules.default = ./nix/module.nix;

      # The library's tests and the example's run in the program's build; the
      # site's build is the example's, and the VM test runs the module.
      checks = forAllSystems (pkgs: {
        inherit (self.packages.${pkgs.stdenv.hostPlatform.system}) program site;
        module = import ./nix/test.nix {
          inherit pkgs;
          module = self.nixosModules.default;
          inherit (self.packages.${pkgs.stdenv.hostPlatform.system}) site program;
          content = ./example/content;
        };
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = with pkgs; [
            git # the receiver's tests
            go
            gopls
          ];
          env = {
            CGO_ENABLED = "0";
            GOTOOLCHAIN = "local";
          };
        };
      });

      apps = forAllSystems (pkgs: {
        # The example site on http://localhost:8080 (or SMALL_PRESS_LISTEN),
        # built again on every request, with the templates read from disk.
        dev = {
          type = "app";
          program = pkgs.lib.getExe (
            pkgs.writeShellApplication {
              name = "small-press-dev";
              runtimeInputs = with pkgs; [
                git
                go
              ];
              text = ''
                cd "$(git rev-parse --show-toplevel)"
                export CGO_ENABLED=0 GOTOOLCHAIN=local
                go run ./example -content example/content -serve "''${SMALL_PRESS_LISTEN:-127.0.0.1:8080}" -dev-assets example
              '';
            }
          );
        };
      });
    };
}
