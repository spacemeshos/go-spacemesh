# Release

## Releasing a major version

Rename UNRELEASED to a concrete <version> and create a PR with commit: Release <version>.
Branch name should match the <version> set in CHANGELOG.
Additionally tag that same commit with 0 as a patch version (if branch is v1.1 - tag is v1.1.0).

## Releasing a minor version

### Latest

Rename UNRELEASED to a concrete <version>. If previous major was v1.1.0, new version will
be v1.1.1.
Commit changes, create pr, and create a tag.
Rebase released branch onto develop.

This is a simplified workflow to avoid cherry-picks and maintaining changelog in several branches.

### Previous

Cherry-pick selected commits to major release branch.
Copy selected commits to CHANGELOG.md.
Rename UNRELEASED to a concrete <version>.
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
