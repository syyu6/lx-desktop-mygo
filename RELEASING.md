# Releasing

This is a personal fork, released from `syyu6/lx-desktop-mygo` on the
`dev/lx-desktop-mygo` branch. The macOS app is signed ad hoc, not notarized:
Gatekeeper asks the first time it is opened.

## Once

`mygo build -upload` signs the updates with the key of `go tool mygo keygen`,
kept in `~/Library/Application Support/mygo/update-keys/`. `mygo-update.pub`
is already in `mygo.json`; put the private key in the repository's secret:

```bash
gh secret set MYGO_UPDATER_PRIVATE_KEY --repo syyu6/lx-desktop-mygo \
  < "$HOME/Library/Application Support/mygo/update-keys/mygo-update.key"
```

Installed apps only accept updates signed with this key, so losing it strands
them. No other secret is needed.

## A release

1. Commit pending work and run `go test ./internal/...`.

2. Bump `version` in `mygo.json` and add a `## 0.1.24` section at the top of
   `CHANGELOG.md` saying, in Chinese, what changed since the last release.
   The workflow refuses a tag that is not `v` + the version, and `mygo build`
   fails without the section: it is what the update window and the release
   page show. Commit both in a commit titled just the version, e.g. `0.1.24`.

3. Push the branch, then the tag:

   ```bash
   git push origin dev/lx-desktop-mygo
   git tag v0.1.24
   git push origin v0.1.24
   ```

4. `.github/workflows/release.yml` builds macOS, Windows and Linux and uploads
   them to a draft release. Wait for every job to pass:

   ```bash
   gh run watch "$(gh run list --workflow release.yml -L1 --json databaseId -q '.[0].databaseId')" --exit-status
   ```

5. Publish the draft and mark it latest:

   ```bash
   gh release edit v0.1.24 --draft=false --latest
   ```

6. Check the update feed shows the new version:

   ```bash
   curl -sL https://github.com/syyu6/lx-desktop-mygo/releases/latest/download/update-darwin-arm64.json
   ```
