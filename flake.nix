{
  description = "corgi — multi-service / db / tunnel orchestrator from a single yml";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        packages.default = pkgs.buildGoModule {
          pname = "corgi";
          version = "1.16.2";
          src = ./.;
          # First build will fail and print the correct hash to paste here.
          # Bump on every dependency change.
          vendorHash = pkgs.lib.fakeHash;
          subPackages = [ "." ];
          ldflags = [ "-s" "-w" ];
          doCheck = false;
          meta = with pkgs.lib; {
            description = "Send someone your project yml file, init and run it in minutes";
            homepage = "https://github.com/Andriiklymiuk/corgi";
            license = licenses.mit;
            mainProgram = "corgi";
          };
        };

        apps.default = flake-utils.lib.mkApp {
          drv = self.packages.${system}.default;
        };

        devShells.default = pkgs.mkShell {
          packages = with pkgs; [ go_1_25 gopls ];
        };
      });
}
