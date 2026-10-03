{
  description = "clonager: keep track of the git clones on your laptop";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";

  outputs = { self, nixpkgs }:
    let
      systems = [ "aarch64-darwin" "x86_64-darwin" "aarch64-linux" "x86_64-linux" ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});

      # A release commit (made by the Release workflow, and only reachable
      # from its tag) has a VERSION file naming the tag, e.g. v0.2.0; main
      # doesn't, so builds from it are named by commit.
      release = if builtins.pathExists ./VERSION then nixpkgs.lib.trim (builtins.readFile ./VERSION) else "";
      version = if release != "" then release else self.shortRev or self.dirtyShortRev or "dev";
    in
    {
      packages = forAllSystems (pkgs: {
        default = pkgs.buildGoModule {
          pname = "clonager";
          version = nixpkgs.lib.removePrefix "v" version;
          src = self;

          # Update when go.mod/go.sum change: set to pkgs.lib.fakeHash, build,
          # and copy the hash from the error.
          vendorHash = "sha256-wkNEgpGpyZJgyLmnuiTzlLJ2xdq9xl6dGGVuyQN1cxE=";

          env.CGO_ENABLED = 0;
          ldflags = [ "-s" "-w" "-X github.com/adnathanail/clonager/cmd.stampedVersion=${version}" ];

          # The tests build throwaway git repos. At runtime clonager uses the
          # git, gh and but on your PATH, so they aren't bundled.
          nativeCheckInputs = [ pkgs.git ];

          # The short name.
          postInstall = ''
            ln -s clonager $out/bin/cg
          '';

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

      # Home Manager module: installs clonager and its config. The installed
      # config is read-only (a copy in the Nix store, or e.g. a secret agenix
      # decrypts), so it only changes on rebuild; with configSource set,
      # `clonager config` edits the source in your checkout instead (directly,
      # or through decrypt and encrypt commands), for you to review and apply.
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
              description = ''
                clonager's config (YAML), installed as
                ~/.config/clonager/config.yaml. A path (./clonager.yaml) is
                copied into the Nix store; a string ("/run/agenix/clonager")
                is linked to where it is, e.g. for a secret decrypted at
                activation.
              '';
            };

            configSource = lib.mkOption {
              type = lib.types.nullOr (lib.types.either lib.types.str (lib.types.submodule {
                options = {
                  decrypt = lib.mkOption {
                    type = lib.types.str;
                    description = "Shell command printing the config (YAML) on stdout. Mustn't prompt.";
                  };
                  encrypt = lib.mkOption {
                    type = lib.types.str;
                    description = "Shell command reading the new config on stdin and storing it. Mustn't prompt.";
                  };
                };
              }));
              default = null;
              example = lib.literalExpression ''
                # A plain file:
                "''${config.home.homeDirectory}/.config/nix-darwin/clonager.yaml"
                # Or one kept encrypted, e.g. with agenix:
                {
                  decrypt = "cd ~/.config/nix-darwin/secrets && agenix -d clonager.age -i ~/.config/age/keys.txt";
                  encrypt = "cd ~/.config/nix-darwin/secrets && agenix -e clonager.age -i ~/.config/age/keys.txt";
                }
              '';
              description = ''
                Where commands that change the config (`clonager config ...`)
                read and write it, as the installed config is read-only;
                rebuild to apply their changes. Either an absolute path to the
                config in your checkout (a string, not a path, so it isn't
                copied into the store), or commands to decrypt and encrypt it.
                The encrypt command only runs when the config changed.
              '';
            };

            discoverPaths = lib.mkOption {
              type = lib.types.listOf lib.types.str;
              default = [ ];
              example = [ "~/Documents" "~/.config/nix-darwin" ];
              description = ''
                Where `clonager config discover` looks when it's given no dirs.
                Absolute or ~ paths; any that don't exist are skipped.
              '';
            };

            openIn = lib.mkOption {
              type = lib.types.nullOr (lib.types.enum [ "vscode" "cursor" "zed" "files" "none" ]);
              default = null;
              example = "vscode";
              description = ''
                What cmd-clicking a repo's name opens its folder in: an editor,
                the file manager (`files`), or nothing (`none`). Null means
                the file manager.
              '';
            };
          };

          config = lib.mkIf cfg.enable {
            assertions = [
              {
                assertion = !(builtins.isString cfg.configSource) || cfg.configFile != null;
                message = "programs.clonager.configSource (a path) needs programs.clonager.configFile.";
              }
              {
                assertion = !(builtins.isString cfg.configSource) || lib.hasPrefix "/" cfg.configSource;
                message = "programs.clonager.configSource must be an absolute path.";
              }
            ];

            home.packages = [ cfg.package ];

            xdg.configFile."clonager/config.yaml" = lib.mkIf (cfg.configFile != null) {
              source =
                if builtins.isPath cfg.configFile then cfg.configFile
                else config.lib.file.mkOutOfStoreSymlink cfg.configFile;
            };
            # config.ReadSettings in the Go code.
            xdg.configFile."clonager/settings.json" =
              let
                settings =
                  lib.optionalAttrs (cfg.configSource != null) {
                    source =
                      if builtins.isString cfg.configSource then { path = cfg.configSource; }
                      else { inherit (cfg.configSource) decrypt encrypt; };
                  }
                  // lib.optionalAttrs (cfg.discoverPaths != [ ]) { inherit (cfg) discoverPaths; }
                  // lib.optionalAttrs (cfg.openIn != null) { inherit (cfg) openIn; };
              in
              lib.mkIf (settings != { }) { text = builtins.toJSON settings; };
          };
        };

      # The older name for homeModules, still what many configs use.
      homeManagerModules = self.homeModules;
    };
}
