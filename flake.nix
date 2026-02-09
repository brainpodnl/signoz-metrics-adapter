{
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
    systems.url = "github:nix-systems/default";
    steiger.url = "github:brainhivenl/steiger";
    filter.url = "github:numtide/nix-filter";
  };

  outputs = {
    self,
    nixpkgs,
    systems,
    steiger,
    filter,
    ...
  }: let
    inherit (nixpkgs) lib;

    overlays = [steiger.overlays.ociTools];
    forAllSystems = fun:
      lib.genAttrs (import systems) (system:
        fun (import nixpkgs {
          inherit system overlays;
        }));
  in {
    devShells = forAllSystems (pkgs: {
      default = pkgs.mkShell {
        packages = [
          pkgs.go
          pkgs.gopls
          pkgs.golangci-lint
          steiger.packages.${pkgs.stdenv.hostPlatform.system}.default
        ];
      };
    });

    packages = forAllSystems (pkgs: {
      default = pkgs.callPackage ./package.nix {inherit filter;};
    });

    steigerImages = steiger.lib.eachCrossSystem (import systems) (localSystem: crossSystem: let
      pkgs = import nixpkgs {
        inherit overlays;
        system = localSystem;
      };
      pkgsCross = import nixpkgs {
        inherit overlays crossSystem localSystem;
      };

      adapter = pkgsCross.callPackage ./package.nix {inherit filter;};
    in {
      adapter = pkgs.ociTools.buildImage {
        name = adapter.pname;
        tag = "latest";
        created = "now";
        copyToRoot = [
          adapter
          pkgs.dockerTools.caCertificates
        ];
        config.Cmd = ["/bin/${adapter.pname}"];
      };
    });
  };
}
