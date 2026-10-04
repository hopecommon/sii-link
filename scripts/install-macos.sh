#!/bin/sh

set -eu

version="${SII_LINK_VERSION:-latest}"
install_dir="${SII_LINK_INSTALL_DIR:-$HOME/.local/bin}"
arch=$(uname -m)

case "$arch" in
    arm64) go_arch=arm64 ;;
    x86_64) go_arch=amd64 ;;
    *)
        echo "Unsupported macOS architecture: $arch" >&2
        exit 1
        ;;
esac

asset="sii-link_darwin_${go_arch}.zip"
if [ "$version" = latest ]; then
    base_url="https://github.com/hopecommon/sii-link/releases/latest/download"
else
    base_url="https://github.com/hopecommon/sii-link/releases/download/$version"
fi

work_dir=$(mktemp -d)
trap 'if [ -d "$work_dir" ]; then rm -r "$work_dir"; fi' EXIT HUP INT TERM

curl -fL --retry 3 -o "$work_dir/$asset" "$base_url/$asset"
curl -fL --retry 3 -o "$work_dir/$asset.sha256" "$base_url/$asset.sha256"
(
    cd "$work_dir"
    shasum -a 256 -c "$asset.sha256"
    unzip -q "$asset"
)

mkdir -p "$install_dir"
if [ -f "$HOME/.local/state/sii-link/gateway.json" ] && \
   ! "$work_dir/sii-link_darwin_${go_arch}/sii-link" -version | grep -qx 'Gateway protocol: 1'; then
    echo 'This release lacks gateway support; preserving the managed installation.' >&2
    exit 1
fi
install -m 0755 "$work_dir/sii-link_darwin_${go_arch}/sii-link" "$install_dir/sii-link"
if [ -f "$work_dir/sii-link_darwin_${go_arch}/sii" ]; then
    install -m 0755 "$work_dir/sii-link_darwin_${go_arch}/sii" "$install_dir/sii"
fi
"$install_dir/sii-link" -version
