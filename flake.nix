{
  description = "clonager: keep track of the git clones on your laptop";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";

  outputs = { self, nixpkgs }:
    let
      systems = [ "aarch64-darwin" "x86_64-darwin" "aarch64-linux" "x86_64-linux" ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
      version = self.shortRev or self.dirtyShortRev or "dev";
    in
    {
      packages = forAllSystems (pkgs: {
        default = pkgs.buildGoModule {
          pname = "clonager";
          inherit version;
          src = self;

          # Update when go.mod/go.sum change: set to pkgs.lib.fakeHash, build,
          # and copy the hash from the error.
          vendorHash = "sha256-haYm6K44hDagVNx5D0tRfc8uLjTwrbiFhiNqKiBD5Uo=";

          env.CGO_ENABLED = 0;
          ldflags = [ "-s" "-w" "-X github.com/adnathanail/clonager/cmd.version=${version}" ];

          # The tests build throwaway git repos. At runtime clonager uses the
          # git, gh and but on your PATH, so they aren't bundled.
          nativeCheckInputs = [ pkgs.git ];

          meta = {
            description = "Keep track of the git clones on your laptop";
            homepage = "https://github.com/adnathanail/clonager";
            mainProgram = "clonager";
          };
        };
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = [ pkgs.go pkgs.gotools pkgs.golangci-lint pkgs.actionlint ];
        };
      });

      # Home Manager module: installs clonager and its config. The config is
      # copied into the Nix store, so it only changes on rebuild; with
      # configSource set, `clonager discover` edits that file in your checkout
      # instead, for you to review and apply.
      homeModules.default = { config, lib, pkgs, ... }:
        let
          cfg = config.programs.clonager;
        in
        {
          options.programs.clonager = {
            enable = lib.mkEnableOption "clonager";

            package = lib.mkOption {
              type = lib.types.package;
              default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
              defaultText = lib.literalExpression "clonager.packages.\${system}.default";
              description = "The clonager package to install.";
            };

            configFile = lib.mkOption {
              type = lib.types.nullOr lib.types.path;
              default = null;
              example = lib.literalExpression "./clonager.yaml";
              description = "clonager's config (YAML), installed as ~/.config/clonager/config.yaml.";
            };

            configSource = lib.mkOption {
              type = lib.types.nullOr lib.types.str;
              default = null;
              example = lib.literalExpression ''"''${config.home.homeDirectory}/.config/nix-darwin/clonager.yaml"'';
              description = ''
                Absolute path to configFile in your checkout, outside the Nix
                store. `clonager discover` adds new repos there; rebuild to
                apply them. A string, not a path, so it isn't copied into the
                store.
              '';
            };
          };

          config = lib.mkIf cfg.enable {
            assertions = [
              {
                assertion = cfg.configSource == null || cfg.configFile != null;
                message = "programs.clonager.configSource needs programs.clonager.configFile.";
              }
              {
                assertion = cfg.configSource == null || lib.hasPrefix "/" cfg.configSource;
                message = "programs.clonager.configSource must be an absolute path.";
              }
            ];

            home.packages = [ cfg.package ];

            xdg.configFile."clonager/config.yaml" = lib.mkIf (cfg.configFile != null) {
              source = cfg.configFile;
            };
            # Where clonager discover writes (see config.Source in the Go code).
            xdg.configFile."clonager/source" = lib.mkIf (cfg.configSource != null) {
              text = cfg.configSource + "\n";
            };
          };
        };

      # The older name for homeModules, still what many configs use.
      homeManagerModules = self.homeModules;
    };
}
