# Spacemesh Release Guide

Welcome! This guide is targeted at those who lead release.
The guide assumes the last released version was v1.2.3.

## Overview

The release cycle takes a new node version and deploys a network for it. 
While `go-spacemesh` is the main program the network is more than 
just a node. There are a number of components that all need to work
together for a smeshnet to work, including poet, smapp, the testnet tap,
explorer backend, explorer frontend, dashboard, monitoring, go-spacecraft, and
CLIWallet.

## Review

Starting on the `develop` branch, run `git log v1.2.3..HEAD`.
Review the changes from the last release and decide which are the notable ones.
Leave out those commits that deal with refactoring, tests and all the plumbing.
For each of the porcelin changes found:
- locate the issue that it solves 
- search for the issue in CHANGELOG.md if it's there you're done
- Add a line in the changelog  the `Unreleased` version. 

When writing the changelog please follow the best practices from
https://keepachangelog.com :

Principles

- Changelogs are for humans, not machines.
- There should be an entry for every single version.
- The same types of changes should be grouped.
- Versions and sections should be linkable.
- The latest version comes first.
- The release date of each version is displayed.

Types of changes

- `Added` for new features.
- `Changed` for changes in existing functionality.
- `Deprecated` for soon-to-be removed features.
- `Removed` for now removed features.
- `Fixed` for any bug fixes.
- `Security` in case of vulnerabilities.

## Name

We follow [SemVer](https://semver.org) so to choose and a new name,
you need to know a few things about the included changes.
Does any of them breaks comptability, in other words, can the new version join
a network running `1.2.3`?. If it can't than the new version is `2.0.0`.
If it can, scan the changelog to see if any of the changes adds a new feature.
If it does the version will benamed `1.3.0`.

If the version doesn't break comptability and doesn't introduce any new feature
then it's `v1.2.4`.

Once you have the name, you update the changelog, replacing the `Unreleased`
version with `1.2.4` and entering the date. Next run:

```
printf "v1.2.4" > version.txt
git commit -am "Releasing v1.2.4"
git tag -m "A short version description" v1.2.4 
git push origin v1.2.4

## Build

Once you push your tag to spacemeshio's fork, a two workflows are triggered :
- "push to dockerhub" - Publishing a docker image of the new node
- "Build and Release" - which builds the node, packs the zip, uploads a 
public file storage and creating a github release page

In the first build the version is named `v1.2.3+1`.
Then the manager `git push --tags` to update github which starts building
the binaries and publishing a new release on gh's release page.

[TBD: This is future staff]
## Deploy 

The CI process that deploys a new network is trigger but a SemVer git tag that
includes a build number.
It uses go-spacecraft for the heavy lifting and ends when the network is up
and a static html page with all the network addresses and
configs as published at: `1_2_3_1.net.spacemeshos.io`.

## Smoke Test

Once the network is up, the CI Process will run a small suite of tests to
ensure deployment was a success.
	
## Post-Deployment

After verifying the netwrok is up and running 

- deploy the explorer and dashboard
- deploy the discovery service
- deploy public gRPC service and ensure it's alive
- updating the `devnet` branch to point at the version

## Simmer

Let the network run for at least 7 days while visiting the dashboard daily.
If consensus totally fails (i.e., the verified layer totally stops advancing),
or another serious issue occurs, then it's obviously time to retire that
network. 

## Release
 
Once the manager is satisified with the network's quality he releases it
by tagging it with a version like "v1.2.3". 

## Integrate

SemVer tags with no build number will trigger a pipeline to:

- pointing testnet DNS records at the new network
- generating & publishing an info page at `test.net.spacemesh.io` 
- updating the `testnet` branch to point to the latest tag

## Announce

Manually update the install links in the sire and announce the new version on discord, twitter, etc..

## Retire the old testnet

Make sure that the prior testnet is completely retired at this stage. All
managed nodes should be shut down and logs should be archived for future
reference.

## Handling Failures 

In case of failures of deployment, investigate, fix and release under an incremented 
build number as in `v1.2.3+2`. 

In case of smeshnet failures Write a postmortem, or a
recap (if it died a natural death), and publish it in the Spacemesh blog.
Describe any issues that arose, how we responded to them, and the plan for the
next smeshnet.
