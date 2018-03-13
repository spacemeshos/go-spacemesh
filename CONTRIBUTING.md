# Overview
Thank you for considering to contribute to the go-spacemesh open source project. We welcome contributions large and small!

- go-spacemesh is part of [The Spacemesh open source project](https://spacemesh.io), and is MIT licensed open source software.
- We welcome major contributors to the spacemesh core dev team.
- Please make sure to scan the [issues](https://github.com/spacemeshos/go-spacemesh/issues). 
- Search the closed ones before reporting things, and help us with the open ones.

# Working on an existing open issue
- Make sure that the issue is labled `Help Wanted` before starting to work on it.
- Add a comment in the issue that you are starting to work on it.
- Ask for any information that you feel is missing from the issue for you to be able to work on it.
- Clone branch `Developer` and not `Master` - we follow [gitflow](https://datasift.github.io/gitflow/IntroducingGitFlow.html)
- Create a PR (Pull Request) from your clone and reference the issue number in it.
- Submit your PR for review when you beleive your code is ready to be merged.
- Follow up on code review comments posted by a maintainer on your Pull Request (PR)
- You may add your name and email to AUTHORS when submitting your Pull Request.
- Please run `./ci/validate-gofmt.sh` and `./ci/validate-lin.sh` before submitting a pull request for review.

# Creating a new issue
- Scan both open and closed issues and verify that your new feature, improvement idea or bug fix proposal is not already being worked on progress or was rejected by the maintainers.
- If your feature idea is already discussed in an open issue that join the conversation on the issue and suggest your proposed design or approach. Coordinate with others who actively work on this issue.
- If an existing closed or open issue not found then create a new repo issue. Describe your idea for new feature or for an improvement of an existing feature and the design of the code you'd like to contribute.
- Wait for feedback from one of the mainteners before starting to work on the feature or the bug fix.

# Code Contributions Guidelines
Please follow these guidelines in your code for your PR to be considered for being merged.

1. You must document all methods and functions using the `GO DOC` style.
2. You must provide at least one unit test for each function and method.
3. You must provide at least one intergration test for each feature which invovled more than one function call. Your tests should reflect hte main ways that your code should be used.
4. You should `gofmt` and `golint` your code.
5. Before submitting your PR for review, make sure that all CI tasks pass. If a test fails, commit fixes and wait for the CI to run all build tasks.

# Working with Gitcoin Bounties
We beleive in compensating contributors and collaboratos for their effort. We are using [gitcoin.io](https://gitcoin.io) for compensating with Ethereum cryptocoins for some open issues. The list of open bounties is available [here](https://gitcoin.co/profile/spacemeshos).


