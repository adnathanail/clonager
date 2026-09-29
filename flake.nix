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
    };
}
