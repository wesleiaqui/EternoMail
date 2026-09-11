# Development container: workspace ownership

Run builds as the owner of the checkout. The existing `anclo-dev` Distrobox
maps `weslei` (1000:1000) to host 1000:1000. Its default container user is root
for initialization: bare `podman exec anclo-dev ...` bypasses Distrobox's normal
user selection and must not be used for builds.

Use the checked entry point from the host:

```sh
stat -c '%u:%g %U:%G' .
tools/dev/container-exec.sh sh -c 'cd frontend && npm run check && npm run build && node --test tests/*.test.mjs'
tools/dev/container-exec.sh /home/weslei/go/bin/wails build -m -nosyncgomod -tags webkit2_41,production
tools/dev/container-exec.sh env GOFLAGS=-tags=webkit2_41 go test ./...
find . ! -user "$(id -un)" -not -path './.git/*' -ls
```

The entry point derives UID/GID from the checkout, rejects root and a mismatched
host caller, explicitly passes `--user UID:GID`, and checks the ownership as seen
inside the container before executing anything. `ANCLO_DEV_CONTAINER` can select
another existing container. That container must already have a matching mapping
and a writable user home/cache. The current Distrobox provides `/home/weslei`.

For a new rootless Podman container use `--userns=keep-id` together with
`--user "$(id -u):$(id -g)"`. A numeric `--user` alone is not sufficient for
arbitrary rootless mappings. For Docker with ordinary host mappings, use the same
`--user`; with userns remapping, configure the mapping before mounting the checkout.
Never use `:U` on this checkout (it recursively changes host ownership).

Both Flatpak Docker launchers explicitly select the checkout owner and use a
writable temporary HOME. System package installation happens during image build,
without a workspace mount. Wails is installed in `/usr/local/bin` so builds do
not depend on access to `/root/go`. The archived AppImage launcher follows the
same ownership discipline. Image defaults are non-root; launcher overrides handle
host IDs other than 1000. Distrobox's initialization root user is intentionally
unchanged; use the wrapper for every npm, Node, Go, Wails or generator invocation.

Generated `frontend/public/spellcheck`, `frontend/dist`, and `build/bin` must belong
to the host user. If older artifacts have wrong ownership, stop writers and have
an administrator restore their ownership to the verified checkout UID/GID (only
the affected paths). Do not run builds with sudo, use chmod 777, or hide EACCES in
application scripts. No recursive repair is needed when the find check is empty.
