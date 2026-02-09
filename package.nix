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
    pname = "test-adapter";
    version = "0.0.0-dev";

    src = filter {
      root = ./.;
      include = [
        ./go.mod
        ./go.sum
        ./pkg
        ./hack
        ./test-adapter
      ];
    };

    vendorHash = "sha256-yF159DawYKxuz75W4q69dI60e5DE3TFzNg7zHipXX80=";

    subPackages = ["test-adapter"];

    env = {
      CGO_ENABLED = "0";
      GOOS = goosMap.${parsed.kernel.name};
      GOARCH = goarchMap.${parsed.cpu.name};
    };

    ldflags = ["-s" "-w"];
  }
