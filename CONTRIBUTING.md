# Overview
Thank you for considering to contribute to the go-spacemesh open source project. We welcome contributions large and small and we actively accept contributions.

- go-spacemesh is part of [The Spacemesh open source project](https://spacemesh.io), and is MIT licensed free open source software.
- Please make sure to scan the [issues](https://github.com/spacemeshos/go-spacemesh/issues). 
- Search the closed ones before reporting things, and help us with the open ones.
- Ensure you are able to contribute to MIT licensed free open software (no legal issues please)
- Ask questions or talk about things in [Issues](https://github.com/spacemeshos/go-spacemesh/issues) or on [Spacemash 
gitter](https://gitter.im/spacemesh-os/Lobby).
- We welcome major contributors to the Spacemesh core dev team.
- **Have fun hacking away our blockchain future!**

# Not sure how to start?
- Look for open issues labeled [Help Wanted](https://github.com/spacemeshos/go-spacemesh/issues)
- Still not sure? Add tests. One can never be enough of them.

# Contributors Compensation
- We believe in compensating contributors and collaborators for their contributions.
- We are using [gitcoin.io](https://gitcoin.io) for compensating with Ethereum cryptocoins for some open issues. The list of open bounties is available [here](https://gitcoin.co/profile/spacemeshos).
- We are experimenting with bounties and we plan to announce additional compensation plans in the near future.

# Working on an existing issue
- Make sure that the issue is labeled `Help Wanted` before starting to work on it.
- Add a comment in the issue that you are starting to work on it.
- Ask for any information that you feel is missing from the issue for you to be able to work on it.
- Clone branch `Developer` and not `Master` - we follow [gitflow](https://datasift.github.io/gitflow/IntroducingGitFlow.html)
- Create a PR (Pull Request) from your clone and reference the issue number in it.
- Submit your PR for review when you believe your code is ready to be merged.
- Follow up on code review comments posted by a maintainer on your Pull Request (PR)
- You may add your name and email to AUTHORS when submitting your Pull Request.
- Please run `./ci/validate-gofmt.sh` and `./ci/validate-lin.sh` before submitting a pull request for review.

# Creating a new issue
- Scan both open and closed issues and verify that your new feature, improvement idea or bug fix proposal is not already being worked on progress or was rejected by the maintainers.
- Before starting to work on a large contribution please chat with the core dev team on our gitter channel to get some initial feedback prior to doing lots of work.
- If your feature idea is already discussed in an open issue then join the conversation on the issue and suggest your proposed design or approach. Coordinate with others who actively work on this issue.
- If an existing closed or open issue not found then create a new repo issue. Describe your idea for new feature or for an improvement of an existing feature and the design of the code you'd like to contribute.
- Wait for feedback from one of the maintainers before starting to work on the feature or the bug fix.

# Code Contributions Guidelines
Please follow the following guidelines to have your PR considered for merging into the project.

1. You should document all methods and functions using [go commentary](https://golang.org/doc/effective_go.html#commentary).  
2. You should provide at least one unit test for each function and method.
3. You should provide at least one integration test for each feature which involves more than one function call. Your tests should reflect the main ways that your code should be used.
4. You should `gofmt` and `golint` your code.
5. Before submitting your PR for review, make sure that all CI tasks pass. If a test fails, commit fixes and wait for the CI to run all build tasks.

# Adding new dependencies
- Check for 3rd-party packages in the vendor folder before adding a new 3rd party dependency.
- Add any new 3rd-party package required by your code to vendor.json - please don't use any other kind of deps importing.

# Working on a funded issue 
- Locate a gitcoin funded issue that you like to work on by browsing the [funded issues](https://github.com/spacemeshos/go-spacemesh/labels/funded) in our github repo, or browse the [list of our funded issues on gitcoin.io]((https://gitcoin.co/profile/spacemeshos) .
- Skip issues that another contributor is already actively working on.
- Click the `Start Work` button on the gitcoin.io issue page when you are ready to start working on the issue.
- Follow the `working on an existing issue guidelines` and the `code contributions guidelines`.
- Communicate with the maintainers using comments on the issue github page.
- Submit your PR and go through the code review process with one of our maintainers.
- Once your PR is approved you should receive your bounty. 
- Be nice - tell us if you decided to abandon an issue you claimed via an issue comment so other people can claim it.
