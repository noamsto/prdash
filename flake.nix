{
  description = "Lean, worktree-first PR/issue TUI";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-parts.url = "github:hercules-ci/flake-parts";
    treefmt-nix.url = "github:numtide/treefmt-nix";
    treefmt-nix.inputs.nixpkgs.follows = "nixpkgs";
    git-hooks-nix.url = "github:cachix/git-hooks.nix";
    git-hooks-nix.inputs.nixpkgs.follows = "nixpkgs";
  };

  outputs = inputs @ {flake-parts, ...}:
    flake-parts.lib.mkFlake {inherit inputs;} {
      systems = ["x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin"];

      imports = [
        inputs.treefmt-nix.flakeModule
        inputs.git-hooks-nix.flakeModule
      ];

      perSystem = {
        pkgs,
        config,
        ...
      }: let
        # The same dependency pin the binary is built with. buildGoModule wires
        # GOPATH/GOMODCACHE to a fetched, read-only module cache, so the
        # static-analysis checks run offline in the Nix sandbox.
        vendorHash = "sha256-Ptb8rKj4GWBAwSYTLFCB9Z6rN9lLo9H7Mljj9WgmykU=";

        # One static-analysis gate as a checks.<system>.* derivation: it reuses
        # packages.prdash's module cache (same src + vendorHash) and runs the
        # gate in checkPhase, so `nix flake check` enforces it locally and in
        # CI. -race needs cgo; the stdenv provides the C toolchain.
        gate = {
          name,
          check,
          cgo ? false,
          extraCheckInputs ? [],
        }:
          pkgs.buildGoModule {
            pname = "prdash-gate-${name}";
            version = "0.0.0";
            src = ./.;
            inherit vendorHash;
            # git: the localgit/cleanup tests shell out to it (and skip without).
            nativeCheckInputs = [pkgs.git] ++ extraCheckInputs;
            env.CGO_ENABLED =
              if cgo
              then "1"
              else "0";
            checkPhase = ''
              runHook preCheck
              export HOME="$TMPDIR"
              export GOCACHE="$TMPDIR/go-cache"
              # buildGoModule's -trimpath breaks tests that read on-disk assets;
              # drop it for the same reason its own checkPhase does.
              export GOFLAGS=''${GOFLAGS//-trimpath/}
              ${check}
              runHook postCheck
            '';
            meta.description = "prdash ${name} gate";
          };
      in {
        packages.prdash = pkgs.buildGoModule {
          pname = "prdash";
          version = "0.1.0";
          src = ./.;
          inherit vendorHash;
          ldflags = ["-s" "-w"];
          meta = {
            description = "Lean, worktree-first PR/issue TUI";
            mainProgram = "prdash";
          };
        };
        packages.default = config.packages.prdash;

        checks = {
          golangci-lint = gate {
            name = "golangci-lint";
            extraCheckInputs = [pkgs.golangci-lint];
            check = "golangci-lint run ./...";
          };
          nilaway = gate {
            name = "nilaway";
            extraCheckInputs = [pkgs.nilaway];
            check = "nilaway -include-pkgs=github.com/noamsto/prdash ./...";
          };
          go-test-race = gate {
            name = "go-test-race";
            cgo = true;
            check = "go test -race ./...";
          };
        };

        treefmt = {
          projectRootFile = "flake.nix";
          programs = {
            alejandra.enable = true;
            gofmt.enable = true;
          };
        };

        pre-commit.settings.hooks = {
          statix.enable = true;
          deadnix.enable = true;
          alejandra.enable = true;
          typos.enable = true;
          check-merge-conflicts.enable = true;
          trim-trailing-whitespace.enable = true;
        };

        devShells.default = pkgs.mkShell {
          inherit (config.pre-commit) shellHook;
          packages =
            config.pre-commit.settings.enabledPackages
            ++ [
              pkgs.go
              pkgs.gopls
              pkgs.gotools
              pkgs.golangci-lint
              pkgs.nilaway
              config.treefmt.build.wrapper
            ];
        };
      };
    };
}
