//! Test harness for the Wallet program.
//!
//! You can run this script using the following command:
//! ```shell
//! RUST_LOG=info cargo run --package wallet-script --bin execute --release
//! ```
use athena_interface::MockHost;
use athena_sdk::{AthenaStdin, ExecutionClient};
use clap::Parser;

/// The ELF (executable and linkable format) file for the Athena RISC-V VM.
///
/// This file is generated by running `cargo athena build` inside the `program` directory.
pub const ELF: &[u8] = include_bytes!("../../../program/elf/wallet-template");

/// The arguments for the run command.
#[derive(Parser, Debug)]
#[clap(author, version, about, long_about = None)]
struct RunArgs {
    #[clap(long, default_value = "20")]
    owner: String,
}

fn main() {
    // Setup the logger.
    athena_sdk::utils::setup_logger();

    // Parse the command line arguments.
    let args = RunArgs::parse();

    // Setup the execution client.
    let client = ExecutionClient::new();

    // Setup the inputs.
    let mut stdin = AthenaStdin::new();

    // Convert the owner to a public key.
    let owner = args.owner.as_bytes().to_vec();
    stdin.write(&owner);

    // Run the program.
    client
        .execute::<MockHost>(ELF, stdin, None, None, None)
        .expect("failed to run program");
    println!("Successfully executed program!");
}
