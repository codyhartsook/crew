# Releasing multiplayer

Releases are immutable version tags. Build and publish them from the current
`main` branch; do not move or reuse a published tag.

## Publish a release

1. Start from an up-to-date `main` with green CI.

   ```sh
   git switch main
   git pull --ff-only
   ```

2. Create and push the next semantic-version tag.

   ```sh
   git tag -a v0.1.0 -m "v0.1.0"
   git push origin v0.1.0
   ```

3. Open the `release` workflow. Its `verify` job runs tests, vet, installer
   syntax, and `goreleaser check`. The publishing job then waits for the
   `release` environment approval.

4. Approve the deployment after checking the tag and workflow output. GoReleaser
   creates the GitHub Release, platform archives, and `checksums.txt`.

5. Smoke-test the published installer in a clean shell.

   ```sh
   curl -fsSL https://raw.githubusercontent.com/codyhartsook/multiplayer/main/scripts/install.sh | sh
   multiplayer --version
   ```

## If a release is wrong

Do not retag or replace it. Ship the fix as the next patch release, for example
`v0.1.1`. This keeps release artifacts, checksums, and installed versions
traceable.
