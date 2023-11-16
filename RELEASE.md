# Release

## Releasing a major version

Update `CHANGELOG.md` by renaming UNRELEASED to a concrete version `x.y.0` and create a PR with commit: Release `x.y.0`.
Branch name should match the version set in CHANGELOG but only include major and minor version: `vx.y` (e.g. v1.1).
Additionally tag that same commit with `0` as a patch version (if branch is v1.1 - tag is v1.1.0).

## Releasing a minor version

### Latest

Rename UNRELEASED to a concrete `version`. If previous major was v1.1.0, new version will
be v1.1.1.
Commit changes, create pr, and create a tag.
Rebase released branch onto develop.

This is a simplified workflow to avoid cherry-picks and maintaining changelog in several branches.

### Previous

Cherry-pick selected commits to major release branch.
Copy selected commits to CHANGELOG.md.
Rename UNRELEASED to a concrete `version`.
Commit changes, and create a tag.

## Preparing for next release

After releasing a new major or minor version, create a new UNRELEASED section at the top of CHANGELOG.md:

```markdown
## UNRELEASED

### Upgrade information

### Highlights

### Features

### Improvements
```

Additionally if a `dddd_next.sql` exists in `sql/migrations/state` and/or `sql/migrations/local` rename it to
`dddd_vx.y.z.sql` where `x.y.z` is the new version and `dddd` is the next available number.

The first PR that needs a DB migrations creates a new `dddd_next.sql` file in `sql/migrations/state` and/or
`sql/migrations/local`.
