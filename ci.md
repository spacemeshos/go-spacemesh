# Continuous Integration (CI)

This repository currently contains CI configuration for two different CI engines, [Travis CI](https://travis-ci.org/) and [GitHub Actions](https://docs.github.com/en/actions) (GA). Historically, we exclusively used Travis, but we're in the process of migrating to GA. Travis configuration can be found in [.travis.yml](.travis.yml) and GA configuration can be found in [.github/workflows](.github/workflows). At present the configuration for these two engines is nearly identical.

## Workflows

We run two sets of tests in CI, one is for convenience (fast feedback) and the other is a "gatekeeper."

The first set of tests is the unit tests (this refers to the "cheap" tests such as `go fmt`, `go mod tidy` and linting). These get run on every push to a PR.

The second set is a superset of the first, but also includes the expensive system tests. We run the second set (mostly*) as a condition for merging a PR into develop. I'll explain how that's implemented, but first some background.

Ideally we'd run the second set on every PR push and block merging until everything passes. However, we can't because it's prohibitively expensive: each test run like this needs our testing infra exclusively for about an hour. At busy times the queue would explode. We also pay for the infra. Additionally, since anyone on Github can open a PR on our repository, one could theoretically run arbitrary code on our infra if we set it up this way.

So the solution we had before bors was that we ran the system tests manually before merging and had them run automatically after the merge. Developers would sometimes forget to run the tests, or choose not to because they felt their changes shouldn't break anything and then it happened from time to time that the build on develop was broken.

Enter [bors](https://bors.tech/). Bors manages our merges into develop. It only obeys commands from users with push access to the repo, so random devs that open PRs can't run arbitrary code on our infra. But when we tell it we want a PR merged (by sending the command `bors merge` as a comment to a PR) it does the following: it sets up the `staging` branch as an exact replica of the `develop` branch, it merges the PR into `staging` and runs the tests (the second, full set of tests), and if they pass it merges the PR, otherwise it lets us know.

This means that no PR is merged without passing all tests first (incl. system tests), `develop` cannot be broken (in fact we never run any tests after a merge, only before) and devs don't need to run system tests manually. It also solves the problem of coordinating who can use the testing infra (so we don't overload it) and has some nice optimizations for optimistically trying to merge several PRs that wait in line together and fall back to an automatic binary search for the offenders if this fails.

So as far as our CI system is concerned, it needs to run the small set of tests on every push to a PR and the large set of tests only on pushes to the `staging` and `trying` branches (not PRs to these branches, but actual pushes to them). The only thing that needs to happen on a push to `develop` is a push to DockerHub (no tests).

## Testing locally

Mimicking the Travis CI workflow locally is extraordinarily difficult. This is one reason we are migrating to GA instead. Fortunately, there is a robust tool called [`act`](https://github.com/nektos/act) that mimics the GA environment locally. It can be installed using Homebrew by running:

```
brew install nektos/tap/act
```

Note that the default `act` [test runners](https://github.com/nektos/act/blob/master/README.md#runners) (docker images) are _intentionally incomplete._ We recommend using the full test runner to mimic the cloud environment as closely as possible. Note that this image is > 18GB. You can do so by adding the `-P` flag to `act` like such:

```
> act -P nektos/act-environments-ubuntu:18.04 pull_request
```

You can also add this to an [actrc file](https://github.com/nektos/act/blob/master/README.md#configuration) so you don't need to specify it with every command.

See the [full documentation](https://github.com/nektos/act/blob/master/README.md) for more details.

## Secrets

