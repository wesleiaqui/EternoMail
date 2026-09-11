#!/usr/bin/env bash
# Restore, merge and finalize the persistent multi-architecture Flatpak repository.
set -euo pipefail

APP_ID="io.github.wesleiaqui.eternomail"
BRANCH="master"

usage() {
    cat >&2 <<'EOF'
Usage:
  manage-repository.sh restore REPOSITORY PUBLISHED_URL
  manage-repository.sh merge REPOSITORY SOURCE_REPOSITORY ARCH
  manage-repository.sh finalize REPOSITORY
  manage-repository.sh verify REPOSITORY
EOF
    exit 2
}

init_repository() {
    local repository="$1"

    mkdir -p "$repository"
    ostree init --repo="$repository" --mode=archive-z2
}

verify_repository() {
    local repository="$1"
    local arch app_ref

    for arch in x86_64 aarch64; do
        app_ref="app/${APP_ID}/${arch}/${BRANCH}"
        if ! ostree rev-parse --repo="$repository" "$app_ref" >/dev/null; then
            echo "Missing required Flatpak ref: $app_ref" >&2
            return 1
        fi
    done
}

restore_repository() {
    local repository="$1"
    local published_url="${2%/}/"
    local summary_url="${published_url}summary"
    local status

    init_repository "$repository"

    if ! status="$(curl --location --silent --show-error \
        --output /dev/null --write-out '%{http_code}' \
        --header 'Cache-Control: no-cache' "$summary_url")"; then
        echo "Could not check the published Flatpak repository: $summary_url" >&2
        return 1
    fi

    case "$status" in
        200)
            ostree remote add --repo="$repository" --if-not-exists \
                --no-gpg-verify published "$published_url"
            ostree pull --repo="$repository" --mirror --depth=-1 \
                --http-header='Cache-Control=no-cache' published
            ostree remote delete --repo="$repository" published
            ;;
        404)
            echo "No published Flatpak repository found; starting the initial repository."
            ;;
        *)
            echo "Unexpected HTTP status $status for $summary_url; refusing to replace repository history." >&2
            return 1
            ;;
    esac
}

merge_architecture() {
    local repository="$1"
    local source_repository="$2"
    local arch="$3"
    local app_ref="app/${APP_ID}/${arch}/${BRANCH}"
    local candidate
    local -a refs=("$app_ref")

    case "$arch" in
        x86_64|aarch64) ;;
        *)
            echo "Unsupported Flatpak architecture: $arch" >&2
            return 1
            ;;
    esac

    if ! ostree rev-parse --repo="$source_repository" "$app_ref" >/dev/null; then
        echo "Source repository does not contain $app_ref" >&2
        return 1
    fi

    for candidate in "appstream/$arch" "appstream2/$arch"; do
        if ostree rev-parse --repo="$source_repository" "$candidate" >/dev/null 2>&1; then
            refs+=("$candidate")
        fi
    done

    ostree pull-local --repo="$repository" --depth=-1 \
        "$source_repository" "${refs[@]}"
}

finalize_repository() {
    local repository="$1"

    verify_repository "$repository"
    flatpak build-update-repo \
        --title="Eterno Mail" \
        --comment="Official Eterno Mail Flatpak repository" \
        --description="Install and update Eterno Mail from its official Flatpak repository." \
        --homepage="https://app.weslleys.com/" \
        --default-branch="$BRANCH" \
        --generate-static-deltas \
        --static-delta-jobs=2 \
        --prune \
        --prune-depth=2 \
        "$repository"
    verify_repository "$repository"
}

[[ $# -ge 1 ]] || usage
command="$1"
shift

case "$command" in
    restore)
        [[ $# -eq 2 ]] || usage
        restore_repository "$1" "$2"
        ;;
    merge)
        [[ $# -eq 3 ]] || usage
        init_repository "$1"
        merge_architecture "$1" "$2" "$3"
        ;;
    finalize)
        [[ $# -eq 1 ]] || usage
        finalize_repository "$1"
        ;;
    verify)
        [[ $# -eq 1 ]] || usage
        verify_repository "$1"
        ;;
    *)
        usage
        ;;
esac
