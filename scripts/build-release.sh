#!/bin/sh

set -eu

target_os="${GOOS:-$(go env GOOS)}"
target_arch="${GOARCH:-$(go env GOARCH)}"
version="${VERSION:-dev}"
commit_id="${COMMIT_ID:-}"
project_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
dist_dir="${DIST_DIR:-$project_root/dist}"
asset_name="sii-link_${target_os}_${target_arch}"
stage_dir="$dist_dir/$asset_name"
binary_name=sii-link

if [ "$target_os" = windows ]; then
    binary_name=sii-link.exe
fi

if [ -e "$stage_dir" ]; then
    rm -r "$stage_dir"
fi
mkdir -p "$stage_dir/LICENSES"

ldflags="-s -w -buildid= -X main.siiLinkVersion=$version"
if [ -n "$commit_id" ]; then
    ldflags="$ldflags -X main.CommitID=$commit_id"
fi

(
    cd "$project_root"
    CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" \
        go build -trimpath -ldflags "$ldflags" -o "$stage_dir/$binary_name" .
)

cp "$project_root/LICENSE" "$project_root/NOTICE.md" \
    "$project_root/THIRD_PARTY_NOTICES.md" "$project_root/README_en.md" "$stage_dir/"
if [ "$target_os" != windows ]; then
    install -m 0755 "$project_root/scripts/sii" "$stage_dir/sii"
fi
cp "$project_root/LICENSES/README.md" "$stage_dir/LICENSES/README.md"

module_list="$stage_dir/.modules"
(
    cd "$project_root"
    CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" \
        go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}}|{{.Version}}|{{.Dir}}{{end}}{{end}}' .
) | awk 'NF' | sort -u > "$module_list"

while IFS='|' read -r module version_number module_dir; do
    safe_name=$(printf '%s@%s' "$module" "$version_number" | tr '/:' '__')
    destination="$stage_dir/LICENSES/$safe_name"
    mkdir -p "$destination"
    found=false
    for candidate in LICENSE LICENSE.txt LICENSE.md COPYING COPYING.txt NOTICE NOTICE.txt NOTICE.md; do
        if [ -f "$module_dir/$candidate" ]; then
            cp "$module_dir/$candidate" "$destination/$candidate"
            found=true
        fi
    done
    if [ "$found" = false ]; then
        echo "No root license file found for $module $version_number" >&2
        exit 1
    fi
done < "$module_list"
rm "$module_list"

(
    cd "$dist_dir"
    rm -f "$asset_name.zip" "$asset_name.zip.sha256"
    zip -qr "$asset_name.zip" "$asset_name"
    shasum -a 256 "$asset_name.zip" > "$asset_name.zip.sha256"
)

printf '%s\n' "$dist_dir/$asset_name.zip"
