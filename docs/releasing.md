# Releasing crew

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

3. Open the `release` workflow. Its `verify` job runs tests, vet, and
   `goreleaser check`. The publishing job then starts on its own: this repo has
   no required reviewers on the `release` environment.

4. Check the workflow output. GoReleaser creates the GitHub Release, the
   `crew_<os>_<arch>.tar.gz` archives, and `checksums.txt`.

5. Download an archive from the GitHub Release, extract it, and run
   `./crew --version`.

## If a release is wrong

Do not retag or replace it. Ship the fix as the next patch release, for example
`v0.1.1`. This keeps release artifacts, checksums, and installed versions
traceable.
