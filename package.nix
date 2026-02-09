{
  buildGoModule,
  stdenv,
  filter,
  ...
}: let
  inherit (stdenv.targetPlatform) parsed;

  goosMap = {
    linux = "linux";
    darwin = "darwin";
  };

  goarchMap = {
    x86_64 = "amd64";
    aarch64 = "arm64";
  };
in
  buildGoModule {
    pname = "signoz-metrics-adapter";
    version = "0.0.0-dev";

    src = filter {
      root = ./.;
      include = [
        ./go.mod
        ./go.sum
        ./pkg
        ./hack
        ./adapter
      ];
    };

    vendorHash = "sha256-MiFnK1aaS6zmyS4Sav3jA1b6+KDprOOhvtmcA1zFdx0=";

    subPackages = ["adapter"];

    env = {
      CGO_ENABLED = "0";
      GOOS = goosMap.${parsed.kernel.name};
      GOARCH = goarchMap.${parsed.cpu.name};
    };

    ldflags = ["-s" "-w"];
  }
