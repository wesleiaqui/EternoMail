#!/usr/bin/env bash
# Execute workspace commands as its owner, never as container root.
set -euo pipefail
repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
owner_uid="$(stat -c %u "$repo_dir")"
owner_gid="$(stat -c %g "$repo_dir")"
container="${ANCLO_DEV_CONTAINER:-anclo-dev}"
if [[ "$owner_uid" == 0 || "$(id -u)" != "$owner_uid" || "$(id -g)" != "$owner_gid" ]]; then
  echo "Run as the workspace owner ($owner_uid:$owner_gid), not root." >&2
  exit 1
fi
if [[ $# == 0 ]]; then
  echo "Usage: $0 command [args...]" >&2
  exit 2
fi
# Inspect the actual bind-mount view: --user alone is insufficient with
# arbitrary rootless user-namespace mappings.
podman exec --user "$owner_uid:$owner_gid" "$container" sh -c '
  test "$(id -u):$(id -g)" = "$1" &&
  test "$(stat -c %u:%g "$2")" = "$1"
' sh "$owner_uid:$owner_gid" "$repo_dir" || {
  echo "Container UID/GID mapping does not match the workspace; use keep-id." >&2
  exit 1
}
exec podman exec --user "$owner_uid:$owner_gid" --env "PATH=/home/weslei/go/bin:/usr/local/bin:/usr/bin" --workdir "$repo_dir" \
  "$container" "$@"
