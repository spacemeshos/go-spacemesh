# Overview
Thank you for considering to contribute to the go-spacemesh open source project. We welcome contributions large and small and we actively accept contributions.

- go-spacemesh is part of [The Spacemesh open source project](https://spacemesh.io), and is MIT licensed free open source software.
- Please make sure to scan the [open issues](https://github.com/spacemeshos/go-spacemesh/issues). 
- Search the closed ones before reporting things, and help us with the open ones.
- Make sure that you are able to contribute to MIT licensed free open software (no legal issues please).
- Introduce yourself, ask questions about issues or talk about things in our [gitter dev channel](https://gitter.im/spacemesh-os/Lobby).
- We actively welcome major contributors to the Spacemesh core dev team.
- **Have fun hacking away our blockchain future together with us!**

# Getting Started
- Browse [Help wanted issues](https://github.com/spacemeshos/go-spacemesh/labels/help%20wanted) and [good first issues](https://github.com/spacemeshos/go-spacemesh/labels/good%20first%20issue).
- Still not sure? Get yourself familiar with the codebase by writing some tests. We never have enough of them.

# Contributors Compensation
- We believe in compensating contributors and collaborators for their open source contributions.
- We are experimenting with bounties and we plan to announce additional compensation plans in the near future.
- We are using [gitcoin](https://gitcoin.co) for compensating contributors with Ethereum cryptocoins for some open issues.
- Browse our open bounties [here](https://gitcoin.co/profile/spacemeshos).

# Working on an existing issue
- Make sure that the issue is labeled `Help Wanted` before starting to work on it.
- Add a comment in the issue that you are starting to work on it.
- Ask for any information that you feel is missing from the issue for you to be able to work on it.
- Clone branch `Develop` and not `Master` - we follow [gitflow](https://datasift.github.io/gitflow/IntroducingGitFlow.html).
- Create a new PR (Pull Request) from your cloned and reference the issue number in it.
- Submit your PR when you believe your code is ready for code review.
- Follow up on code review comments by one of the project maintainers on your Pull Request (PR)
- You may add your name and email to AUTHORS when submitting your Pull Request.

# Creating a new issue
- Scan both open and closed issues and verify that your new feature idea, improvement proposal or bug report is not already addressed in another issue.
- Before starting to work on a large contribution please chat with the core dev team on our Gitter channel to get some initial feedback prior to doing lots of work.
- If your feature idea is already discussed in an open issue then join the conversation on the issue and suggest your proposed design or approach. Coordinate with others who actively work on this issue.
- If an existing closed or open issue not found then create a new repo issue. Describe your idea for new feature or for an improvement of an existing feature and the design of the code you'd like to contribute.
- Wait for feedback from one of the maintainers before starting to work on the feature or the bug fix. We provide feedback on all issue comments in up to 24 hours.

# Code Guidelines
Please follow these guidelines for your PR to be reviewed and be considered for merging into the project.

1. Document all methods and functions using [go commentary](https://golang.org/doc/effective_go.html#commentary).  
2. Provide at least one unit test for each function and method.
3. Provide at least one integration test for each feature with a flow which involves more than one function call. Your tests should reflect the main ways that your code should be used.
4. Run `go mod tidy`, `go fmt ./...` and `make lint` to format and lint your code before submitting your PR.
5. Make sure that all CI tasks pass. CI is integrated with our github issues. If a test fails, commit a fix and wait for the CI system to build your PR and run all tests.

# Adding new dependencies
- Check for existing 3rd-party packages in the vendor folder `./vendor` before adding a new dependency.
- Use [govendor](https://github.com/kardianos/govendor) to add a new dependency.

# Working on a funded issue 

## Step 1 - Discover :sunrise_over_mountains:
- Browse the [open funded issues](https://github.com/spacemeshos/go-spacemesh/labels/funded%20%3Amoneybag%3A) in our github repo, or on our [gitcoin.io funded issues page](https://gitcoin.co/profile/spacemeshos).
- Skip issues that another contributor is already actively working on.
- Find a funded issue you'd like to be working on.
- Ask any questions you may have about the work involved in the issue github page comments section.
- Click the `Start Work` button on the gitcoin.io issue page when you are ready to start working on the issue.

## Step 2 - Develop :computer:
- Follow the `working on an existing issue` guidelines and the `code guidelines` outlined on this page.
- Communicate with the maintainers in the issue comments area.
- Be nice! please tell us if you decide to abandon an issue you have claimed via an issue comment so other people can start working on it.
- As you work on the issue, please update us on your progress using issue comments.
- If you claim an issue and become irresponsive for more than 7 days then we might encourage other contributors to claim it.

## Step 3 - Get paid :moneybag:
- When ready, submit your PR for review and go through the code review process with one of our maintainers.
- Expect a review process that ensures that you have followed our code guidelines at that your design and implementation are solid. You are expected to revision your code based on reviewers comments.
- You should receive your bounty as soon as your PR is approved and merged by one of our maintainers. 

Please review our funded issues program [legal notes](https://github.com/spacemeshos/go-spacemesh/blob/master/legal.md).
