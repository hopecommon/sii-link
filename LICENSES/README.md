# Release license bundle

The repository's combined license is the root `LICENSE` (GNU AGPLv3).

Official binary packaging copies this marker into the release archive, creates
one adjacent subdirectory for every linked Go module, and copies that module's
root `LICENSE`, `COPYING`, and `NOTICE` files. The generated target-specific
contents are included in release archives rather than committed as duplicated
dependency sources. Run
`scripts/build-release.sh` to reproduce the exact bundle for a target.
