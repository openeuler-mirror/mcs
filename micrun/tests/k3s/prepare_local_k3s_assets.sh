#!/bin/bash
set -euo pipefail

ASSET_DIR="${K3S_LOCAL_ASSET_DIR:-/tmp/micrun-k3s-assets}"
K3S_VERSION="${K3S_LOCAL_VERSION:-v1.27.15+k3s1}"
PAUSE_IMAGE_TAG="${K3S_LOCAL_PAUSE_IMAGE_TAG:-rancher/mirrored-pause:3.6}"
GO_BIN="${GO_BIN:-go}"
DOCKER_BIN="${DOCKER_BIN:-docker}"

VERSION_PATH="${K3S_VERSION//+/%2B}"
K3S_ARM64_URL="https://github.com/k3s-io/k3s/releases/download/${VERSION_PATH}/k3s-arm64"
K3S_AMD64_URL="https://github.com/k3s-io/k3s/releases/download/${VERSION_PATH}/k3s"

ARM_BIN="${ASSET_DIR}/k3s-arm64"
AMD_BIN="${ASSET_DIR}/k3s-amd64"
PAUSE_BIN="${ASSET_DIR}/pause"
PAUSE_TAR="${ASSET_DIR}/pause-image-arm64.tar"
PAUSE_GO="${ASSET_DIR}/pause.go"
PAUSE_DOCKERFILE="${ASSET_DIR}/Dockerfile.pause"

log() {
    printf '[prepare-k3s-assets] %s\n' "$1"
}

require_command() {
    command -v "$1" >/dev/null 2>&1 || {
        printf 'required command not found: %s\n' "$1" >&2
        exit 1
    }
}

file_size() {
    wc -c <"$1" | tr -d '[:space:]'
}

sha256_hex() {
    sha256sum "$1" | awk '{print $1}'
}

write_pause_source() {
    cat >"$PAUSE_GO" <<'EOF'
package main

import (
	"os"
	"os/signal"
	"syscall"
)

func main() {
	sigCh := make(chan os.Signal, 4)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	for {
		<-sigCh
	}
}
EOF
}

write_pause_dockerfile() {
    cat >"$PAUSE_DOCKERFILE" <<'EOF'
FROM scratch
COPY pause /pause
ENTRYPOINT ["/pause"]
EOF
}

build_pause_with_docker() {
    command -v "$DOCKER_BIN" >/dev/null 2>&1 || return 1

    write_pause_dockerfile
    "$DOCKER_BIN" buildx build --platform linux/arm64 --load -t "$PAUSE_IMAGE_TAG" -f "$PAUSE_DOCKERFILE" "$ASSET_DIR" || return 1
    "$DOCKER_BIN" save -o "$PAUSE_TAR" "$PAUSE_IMAGE_TAG"
}

build_pause_oci_tar() {
    local workdir layerdir layer_tar config_json manifest_json
    local layer_digest layer_size config_digest config_size manifest_digest manifest_size

    workdir="$(mktemp -d "${ASSET_DIR}/pause-oci.XXXXXX")"
    layerdir="$(mktemp -d "${ASSET_DIR}/pause-layer.XXXXXX")"
    trap 'rm -rf "$workdir" "$layerdir"' RETURN

    mkdir -p "$workdir/blobs/sha256"
    install -m 755 "$PAUSE_BIN" "$layerdir/pause"

    layer_tar="$workdir/layer.tar"
    tar --sort=name --mtime='UTC 1970-01-01' --owner=0 --group=0 --numeric-owner -C "$layerdir" -cf "$layer_tar" pause
    layer_digest="$(sha256_hex "$layer_tar")"
    layer_size="$(file_size "$layer_tar")"

    config_json="$workdir/config.json"
    cat >"$config_json" <<EOF
{"created":"1970-01-01T00:00:00Z","architecture":"arm64","os":"linux","config":{"Entrypoint":["/pause"]},"rootfs":{"type":"layers","diff_ids":["sha256:${layer_digest}"]},"history":[{"created":"1970-01-01T00:00:00Z","created_by":"micrun local k3s pause image"}]}
EOF
    config_digest="$(sha256_hex "$config_json")"
    config_size="$(file_size "$config_json")"

    manifest_json="$workdir/manifest.json"
    cat >"$manifest_json" <<EOF
{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:${config_digest}","size":${config_size}},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"sha256:${layer_digest}","size":${layer_size}}]}
EOF
    manifest_digest="$(sha256_hex "$manifest_json")"
    manifest_size="$(file_size "$manifest_json")"

    mv "$layer_tar" "$workdir/blobs/sha256/${layer_digest}"
    mv "$config_json" "$workdir/blobs/sha256/${config_digest}"
    mv "$manifest_json" "$workdir/blobs/sha256/${manifest_digest}"
    printf '{"imageLayoutVersion":"1.0.0"}\n' >"$workdir/oci-layout"
    cat >"$workdir/index.json" <<EOF
{"schemaVersion":2,"manifests":[{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"sha256:${manifest_digest}","size":${manifest_size},"platform":{"architecture":"arm64","os":"linux"},"annotations":{"org.opencontainers.image.ref.name":"${PAUSE_IMAGE_TAG}"}}]}
EOF

    tar -C "$workdir" -cf "$PAUSE_TAR" oci-layout index.json blobs
    rm -rf "$workdir" "$layerdir"
    trap - RETURN
}

download_if_missing() {
    local url="$1"
    local dest="$2"

    if [ -x "$dest" ]; then
        return 0
    fi

    log "downloading $(basename "$dest")"
    curl -L --fail --output "$dest" "$url"
    chmod +x "$dest"
}

mkdir -p "$ASSET_DIR"
require_command curl
require_command "$GO_BIN"
require_command tar
require_command sha256sum

download_if_missing "$K3S_ARM64_URL" "$ARM_BIN"
download_if_missing "$K3S_AMD64_URL" "$AMD_BIN"

if [ ! -f "$PAUSE_TAR" ]; then
    log "building local arm64 pause image tar"
    write_pause_source
    GOOS=linux GOARCH=arm64 CGO_ENABLED=0 "$GO_BIN" build -o "$PAUSE_BIN" "$PAUSE_GO"
    if ! build_pause_with_docker; then
        log "docker buildx path unavailable, creating OCI image tar directly"
        build_pause_oci_tar
    fi
fi

log "assets ready:"
ls -lh "$ARM_BIN" "$AMD_BIN" "$PAUSE_TAR"
