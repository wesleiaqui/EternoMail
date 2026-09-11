# Flatpak update repository

Eterno Mail publishes release bundles on GitHub Releases and an OSTree update
repository on GitHub Pages. Both distribution paths use the existing application
ID `io.github.wesleiaqui.eternomail` and branch `master`.

## Published URLs

- Repository: `https://app.weslleys.com/flatpak/repo/`
- Remote descriptor: `https://app.weslleys.com/flatpak/eternomail.flatpakrepo`
- Project website: `https://app.weslleys.com/`

The repository is unsigned. Transport is protected by HTTPS and OSTree verifies
downloaded objects by checksum. The `.flatpakrepo` descriptor configures this
mode automatically; commands that use the raw repository URL must explicitly use
`--no-gpg-verify`. No signing key or GitHub secret is required by the current
release process.

## Release architecture

The tagged-release workflow performs these steps:

1. Build all existing platform artifacts and create the GitHub Release.
2. Prepare the Flatpak manifest and build x86_64 and aarch64 independently.
3. Before each build, mirror the currently published OSTree repository so the new
   commit retains its parent history.
4. Save each architecture's repository and bundle as workflow artifacts.
5. After both builds succeed, one job restores the published repository, imports
   only the new refs from both architectures, verifies both app refs, generates
   static deltas and prunes history beyond two parents.
6. Upload both `.flatpak` bundles to the GitHub Release and deploy the consolidated
   repository together with the existing website to GitHub Pages.

Release workflow runs are serialized. Test tags containing `-test` or `-build`
still build artifacts but do not update the stable Pages repository. If a build or
repository validation fails, neither architecture is deployed and no Flatpak
bundle is added to the release by the Flatpak stage.

The two-parent retention bounds the Pages site size while keeping recent delta
history. An installation on an older commit can still update directly to the
current commit; it may download full objects when an old static delta was pruned.

## One-time GitHub Pages setup

In the GitHub repository, open **Settings → Pages** and set **Build and
deployment → Source** to **GitHub Actions**. Keep the existing custom domain
`app.weslleys.com`; the tracked `CNAME` is included in the generated Pages site.

The workflow uses the `github-pages` environment and grants only `contents: write`
for release bundle upload plus `pages: write` and `id-token: write` for deployment.
No additional secrets are required.

## New installation

For a per-user installation:

```bash
flatpak remote-add --user --if-not-exists --from \
  eternomail-origin \
  https://app.weslleys.com/flatpak/eternomail.flatpakrepo
flatpak install --user eternomail-origin io.github.wesleiaqui.eternomail
```

The equivalent command using the raw repository URL is:

```bash
flatpak remote-add --user --if-not-exists --no-gpg-verify \
  eternomail-origin https://app.weslleys.com/flatpak/repo/
```

Alternatively, installing a v0.3.9-or-newer `.flatpak` bundle from GitHub Releases
creates `eternomail-origin` with the stable repository URL embedded in the bundle.

## Existing installation migration

Old release bundles created an `eternomail-origin` with no URL and marked it
disabled. For a system installation, repair it once with:

```bash
sudo flatpak remote-modify --system \
  --url=https://app.weslleys.com/flatpak/repo/ \
  --enable --no-gpg-verify eternomail-origin
sudo flatpak update --system io.github.wesleiaqui.eternomail
```

For a per-user installation, use:

```bash
flatpak remote-modify --user \
  --url=https://app.weslleys.com/flatpak/repo/ \
  --enable --no-gpg-verify eternomail-origin
flatpak update --user io.github.wesleiaqui.eternomail
```

After that one-time repair, normal updates are:

```bash
flatpak update io.github.wesleiaqui.eternomail
```

## Pre-tag validation

Before creating `v0.3.9`, run the normal project checks and verify version metadata:

```bash
./scripts/version.sh check
./scripts/version.sh check v0.3.9
```

The repository merge logic can be tested without deployment by creating temporary
x86_64 and aarch64 OSTree repositories and running:

```bash
build/flatpak/manage-repository.sh merge TARGET X86_REPOSITORY x86_64
build/flatpak/manage-repository.sh merge TARGET ARM_REPOSITORY aarch64
build/flatpak/manage-repository.sh finalize TARGET
build/flatpak/manage-repository.sh verify TARGET
```

After a test deployment or the v0.3.9 release, verify the hosted summary and both
architectures:

```bash
curl --fail https://app.weslleys.com/flatpak/repo/summary >/dev/null
flatpak remote-info --user eternomail-origin \
  io.github.wesleiaqui.eternomail
flatpak remote-ls --user --arch=x86_64 eternomail-origin
flatpak remote-ls --user --arch=aarch64 eternomail-origin
```

To exercise an actual update before publishing the production tag, deploy the same
workflow temporarily to a separate Pages URL and point an isolated Flatpak test
installation at it. Install the first test revision, publish a second revision,
then confirm that `flatpak update` changes the commit reported by `flatpak info`.
Do not use a `-test` or `-build` tag for this hosted test because those suffixes
intentionally skip stable repository deployment.
