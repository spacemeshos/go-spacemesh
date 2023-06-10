# [Spacemesh: A Programmable Cryptocurrency](https://spacemesh.io)

[![license](https://img.shields.io/packagist/l/doctrine/orm.svg)](https://github.com/spacemeshos/go-spacemesh/blob/master/LICENSE)
[![release](https://img.shields.io/github/v/release/spacemeshos/go-spacemesh?include_prereleases)](https://github.com/spacemeshos/go-spacemesh/releases)
![platform](https://img.shields.io/badge/platform-win--64%20|%20macos--64%20|%20linux--64%20|%20freebsd-lightgrey.svg)
[![go version](https://img.shields.io/github/go-mod/go-version/spacemeshos/go-spacemesh?logo=go)](https://go.dev/)
[![open help wanted issues](https://img.shields.io/github/issues-raw/spacemeshos/go-spacemesh/help%20wanted?logo=github)](https://github.com/spacemeshos/go-spacemesh/issues?q=is%3Aissue+is%3Aopen+label%3A%22help+wanted%22)
[![discord](https://img.shields.io/discord/623195163510046732?label=discord&logo=discord)](http://chat.spacemesh.io/)
[![made by](https://img.shields.io/badge/madeby-spacemeshos-blue.svg)](https://spacemesh.io)
[![Go Report Card](https://goreportcard.com/badge/github.com/spacemeshos/go-spacemesh)](https://goreportcard.com/report/github.com/spacemeshos/go-spacemesh)
[![Bors enabled](https://bors.tech/images/badge_small.svg)](https://app.bors.tech/repositories/22421)
[![godoc](https://img.shields.io/badge/godoc-LGTM-blue.svg)](https://godoc.org/github.com/spacemeshos/go-spacemesh)
[![CI: passing](https://img.shields.io/badge/CI-passing-success?logo=github&style=flat)](https://github.com/spacemeshos/go-spacemesh/blob/develop/ci.md#ci-status)

## go-spacemesh

💾⏰💪

Thanks for your interest in this open source project. This repo is the go implementation of the [Spacemesh](https://spacemesh.io) p2p full node software.

Spacemesh is a decentralized blockchain computer using a new race-free consensus protocol that doesn't involve energy-wasteful `proof of work`.

We aim to create a secure and scalable decentralized computer formed by a large number of desktop PCs at home.

We are designing and coding a modern blockchain platform from the ground up for scale, security and speed based on the learnings of the achievements and mistakes of previous projects in this space.

To learn more about Spacemesh head over to [https://spacemesh.io](https://spacemesh.io).

To learn more about the Spacemesh protocol [watch this video](https://www.youtube.com/watch?v=jvtHFOlA1GI).

### Motivation

Spacemesh is designed to create a decentralized blockchain smart contracts computer and a cryptocurrency that is formed by connecting the home PCs of people from around the world into one virtual computer without incurring massive energy waste and mining pools issues that are inherent in other blockchain computers, and provide a provably-secure and incentive-compatible smart contracts execution environment.

Spacemesh is designed to be ASIC-resistant and in a way that doesn’t give an unfair advantage to rich parties who can afford setting up dedicated computers on the network. We achieve this by using a novel consensus protocol and optimize the software to be most effectively be used on home PCs that are also used for interactive apps.

### What is this good for?

Provide dapp and app developers with a robust way to add value exchange and other value related features to their apps at scale. Our goal is to create a truly decentralized cryptocurrency that fulfills the original vision behind bitcoin to become a secure trustless store of value as well as a transactional currency with extremely low transaction fees.

### Target Users

go-spacemesh is designed to be installed and operated on users' home PCs to form one decentralized computer. It is going to be distributed in the Spacemesh App but people can also build and run it from source code.

### Project Status

We are working hard towards our first major milestone - a public permissionless testnet running the Spacemesh consensus protocol.

### Contributing

Thank you for considering to contribute to the go-spacemesh open source project!

We welcome contributions large and small and we actively accept contributions.

- go-spacemesh is part of [The Spacemesh open source project](https://spacemesh.io), and is MIT licensed open source software.

- We welcome collaborators to the Spacemesh core dev team.

- You don’t have to contribute code! Many important types of contributions are important for our project. See: [How to Contribute to Open Source?](https://opensource.guide/how-to-contribute/#what-it-means-to-contribute)

- To get started, please read our [contributions guidelines](https://github.com/spacemeshos/go-spacemesh/blob/master/CONTRIBUTING.md).

- Browse [Good First Issues](https://github.com/spacemeshos/go-spacemesh/labels/good%20first%20issue).

- Get ethereum awards for your contribution by working on one of our [gitcoin funded issues](https://gitcoin.co/profile/spacemeshos).

### Diggin' Deeper

Please read the Spacemesh [full FAQ](https://github.com/spacemeshos/go-spacemesh/wiki/Spacemesh-FAQ).

### go-spacemesh Architecture

![Architecture](https://raw.githubusercontent.com/spacemeshos/product/master/resources/go-spacemesh-architecture.png)

### Getting

```bash
git clone git@github.com:spacemeshos/go-spacemesh.git
```

or fork the project from <https://github.com/spacemeshos/go-spacemesh>

Since the project uses Go Modules it is best to place the code **outside** your `$GOPATH`. Read [this](https://github.com/golang/go/wiki/Modules#how-to-install-and-activate-module-support) for alternatives.

### Setting Up Local Dev Environment

Building is supported on OS X, Linux, FreeBSD, and Windows.

Install [Go 1.19 or later](https://golang.org/dl/) for your platform, if you haven't already.

On Windows you need to install `make` via [msys2](https://www.msys2.org/), [MingGW-w64](http://mingw-w64.org/doku.php) or [mingw](https://chocolatey.org/packages/mingw)

Ensure that `$GOPATH` is set correctly and that the `$GOPATH/bin` directory appears in `$PATH`.

Before building we need to set up the golang environment. Do this by running:

```bash
make install
```

### How to run standalone node?

After you got a binary standalone fully functional network can be launched
with a simple command:

> ./build/go-spacemesh --preset=standalone --genesis-time=2023-06-08T5:30:00.000Z

Network will use short epochs (1 minute), and 10 layers within the epoch (each 6s). Poet is launched in the same process in this mode. So expect that it will periodically hog 1 core. Minimal smeshig is enabled in order for consensus to work.

Public GRPC API are launched on 0.0.0.0:10092. Private - 0.0.0.0:10093.

### Building

To build `go-spacemesh` for your current system architecture, from the project root directory, use:

```bash
make build
```

(On FreeBSD, you should instead use `gmake build`. You can install `gmake` with `pkg install gmake` if it isn't already installed.)

This will build the `go-spacemesh` binary, saving it in the `build/` directory.

On linux or mac you can build a binary for windows using:

```bash
make windows
```

Be aware that this will require a cross-platform gcc like `x86_64-w64-mingw32-gcc`. Platform-specific binaries are saved to
the `build/*target*` directory.

### Using `go build` and `go test` without `make`

To build or test code without using `make` some golang environment variables
must be set appropriately.

The environment variables can be printed by running either `make print-env` or
`make print-test-env`.

They can be set in 3 ways:

_Note: we need to use eval to interpret the commands since there are spaces in
the values of the variables so the shell can't correctly split them as
arguments._

1. Setting the variables on the same line as the `go` command (e.g., `eval $(make print-env) go build ./...`). This affects the environment for that command invocation only.
2. Exporting the variables in the shell's environment (e.g., `eval export $(make print-env)`). The variables will persist for the duration of that shell (and will be passed to subshells).
3. Setting the variables in the go environment (e.g., `eval go env -w $(make print-env)`). Persistently adds these values to Go's environment for any future runs.

---

### Running

_Note: go-spacemesh relies on a gpu setup dynamic library in order to run.
`make install` puts this file in the build folder, so if you are running
spacemesh from the build folder you don't need to take any extra action.
However if you have built the binary using `go build` or moved the binary from
the build folder you need to ensure that you have the gpu setup dynamic library
(the exact name will vary based on your OS) accessible by the go-spacemesh
binary. The simplest way to do this is just copy the library file to be in the
same directory as the go-spacemesh binary. Alternatively you can modify your
system's library search paths (e.g. LD_LIBRARY_PATH) to ensure that the
library is found._

go-spacemesh is p2p software which is designed to form a decentralized network by connecting to other instances of go-spacemesh running on remote computers.

To run go-spacemesh you need to specify the parameters shared between all instances on a specific network.

You specify these parameters by providing go-spacemesh with a json config file. Other CLI flags control local node behavior and override default values.

#### Joining a Testnet (without mining)

1. Build go-spacemesh from source code.
2. Download the testnet's json config file. Make sure your local config file suffix is `.json`.
3. Start go-spacemesh with the following arguments:

    ```bash
    ./go-spacemesh --listen [a_multiaddr] --config [configFileLocation] -d [nodeDataFilesPath]
    ```

    **Example:**

    Assuming `tn1.json` is a testnet config file saved in the same directory as go-spacemesh, use the following command to join the testnet. The data folder will be created in the same directory as go-spacemesh. The node will use TCP port 7513 and UDP port 7513 for p2p connections:

    ```bash
    ./go-spacemesh --listen /ip4/0.0.0.0/tcp/7513 --config ./tn1.json -d ./sm_data
    ```

4. Build the [CLI Wallet](https://github.com/spacemeshos/CLIWallet) from source code and run it:
5. Use the CLI Wallet commands to setup accounts, start smeshing and execute transactions.

```bash
./cli_wallet
```

#### Joining a Testnet (with mining)

1. Run go-spacemesh to join a testnet without mining (see above).
2. Run the CLI Wallet to create a coinbase account. Save your coinbase account public address - you'll need it later.
3. Stop go-spacemesh and start it with the following params:

    ```bash
    ./go-spacemesh --listen [a_multiaddr] --config [configFileLocation] -d [nodeDataFilesPath] --smeshing-coinbase [coinbase_account] --smeshing-start --smeshing-opts-datadir [dir_for_post_data]
    ```

    **Example:**

    ```bash
    ./go-spacemesh --listen /ip4/0.0.0.0/tcp/7513 --config ./tn1.json -d ./sm_data --smeshing-coinbase stest1qqqqqqp3qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqql50dsa --smeshing-start --smeshing-opts-datadir ./post_data
    ```

4. Use the CLI wallet to check your coinbase account balance and to transact

---

### Smeshing

To be able to initialize your PoST using your Graphics card you will need to install the tools necessary to enable OpenCL support on your system.
The exact steps to do this will vary based on your OS and GPU. In general you will need to install the OpenCL runtime for your GPU and ICD loader.

A good starting point to get more info is <https://wiki.archlinux.org/title/GPGPU>.

If your system doesn't have a GPU or you can use a generic runtime instead. Be aware that we do not recommend this for initialization of PoST.
On Ubuntu you need to install the following packages:

```bash
apt-get update
apt-get install libpocl2
```

on Windows you can use Intel OpenAPI:

```bash
choco install opencl-intel-cpu-runtime
```

---

### Testing

_NOTE_: if tests are hanging try running `ulimit -n 400`. some tests require that to work.

```bash
TEST_LOG_LEVEL="" make test
```

The optional `TEST_LOG_LEVEL` environment variable can be set to change the log level during test execution.
If not set, tests won't print any logs. Valid values are the error levels of [zapcore](https://pkg.go.dev/go.uber.org/zap/zapcore#Level)

For code coverage you can run:

```bash
make cover
```

This will start a local web service and open your browser to render a coverage report. If you just want to
generate a cover profile you can run:

```bash
make cover-profile
```

The generated file will be saved to `./cover.out`. It can be loaded into your editor or IDE to view which code paths
are covered by tests and which not.

### Continuous Integration

We've enabled continuous integration on this repository in GitHub. You can read more about [our CI workflows](ci.md).

### Docker

A `Dockerfile` is included in the project allowing anyone to build and run a docker image:

```bash
docker build -t spacemesh .
docker run -d --name=spacemesh spacemesh
```

### Windows

On Windows you will need the following prerequisites:

- Powershell - included by in Windows by default since Windows 7 and Windows Server 2008 R2
- [Git for Windows](https://gitforwindows.org/) - after installation remove `C:\Program Files\Git\bin` from [System PATH](https://www.java.com/en/download/help/path.xml) (if present) and add `C:\Program Files\Git\cmd` to System PATH (if not already present)
- [Make](http://gnuwin32.sourceforge.net/packages/make.htm) - after installation add `C:\Program Files (x86)\GnuWin32\bin` to System PATH
- [Golang](https://golang.org/dl/)
- GCC. There are several ways to install gcc on Windows, including Cygwin. Instead, we recommend [tdm-gcc](https://jmeubank.github.io/tdm-gcc/) which we've tested.

Close and reopen powershell to load the new PATH. You can then run the command `make install` followed by `make build` as on UNIX-based systems.

### Running a Local Testnet

- You can run a local Spacemesh Testnet with 6 full nodes, 6 user accounts, and 1 POET support service on your computer using docker.
- The local testnet full nodes are built from this repo.
- This is a great way to get a feel for the protocol and the platform and to start hacking on Spacemesh.
- Follow the steps in our [Local Testnet Guide](https://testnet.spacemesh.io/#/README)

#### Next Steps

- Please visit our [wiki](https://github.com/spacemeshos/go-spacemesh/wiki)
- Browse project [go docs](https://godoc.org/github.com/spacemeshos/go-spacemesh)
- Spacemesh Protocol [video overview](https://www.youtube.com/watch?v=jvtHFOlA1GI)

### Got Questions?

- Introduce yourself and ask anything on [Discord](http://chat.spacemesh.io/).
- DM [@teamspacemesh](https://twitter.com/teamspacemesh)
