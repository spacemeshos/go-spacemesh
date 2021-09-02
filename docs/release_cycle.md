# Spacemesh Release Cycle

The release cycle takes a new node version and deploys a network for it. 
While `go-spacemesh` is the main program the smeshnet is more than 
just a node. There are a number of components that all need to work
together for a smeshnet to work, including poet, smapp, the testnet tap, explorer backend,
explorer frontend, dashboard, monitoring, go-spacecraft, and CLIWallet.

## Review & Name

When it's time to release a version, the release manager kicks off by reviewing the 
git log, comparing to the [changelog](https://keepachangelog.com/en/1.0.0/),
polishing it and naming the version.
We follow [SemVer](https://semver.org) so to choose and a new name,
the managers starts with the last name and:

- If any of the included PRs break compatbility, increment the major
- If any of the included PRs add a new feture, increment the minor
- If they just fix bugs, increment the patch

Assuming the chosen name is "1.2.3", the release manager will
updated the changelog, replacing the `Unreleased` version
with `1.2.3` and enter its date. The he committs this and all other edits as
"Releasing v1.2.3".

## Build

Once the manager is finished he tags the version with the name + a build number.
In the first build the version is named `v1.2.3+1`.
Then the manager `git push --tags` to update github and start the pipeline.

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
