# rancher-ci-image

Container images used by Rancher's CI pipelines, published to `ghcr.io/macedogm/ci-image`.

## Available Images

| Image Tag | Go Version | Description |
|-----------|------------|-------------|
| `go1.25`  | 1.25.9     | CI image with Go 1.25 toolchain |
| `go1.26`  | 1.26.2     | CI image with Go 1.26 toolchain |

## How It Works

1. Tool versions and checksums are defined in [`deps.yaml`](deps.yaml).
2. Dockerfile templates live in [`templates/`](templates/).
3. `make generate` renders the templates into `dockerfiles/Dockerfile.*` files using the Go generator in [`main.go`](main.go).
4. `make verify` ensures no uncommitted changes exist (used in CI).
5. `make push` builds and pushes multi-arch images to GHCR.

## Usage

```bash
# Generate Dockerfiles from templates
make generate

# Build all images locally
make build

# Build and push to registry
make push REPO=ghcr.io/macedogm/ci-image

# Run the full workflow
make all
```

## Adding a New Tool

1. Add an entry to `deps.yaml` with the tool name, version, checksums, and download template.
2. Run `make generate` and commit the updated Dockerfiles.

## Adding a New Go Version

1. Create a new template in `templates/Dockerfile.goX.YZ.tmpl`.
2. Run `make generate` and commit the new Dockerfile.
