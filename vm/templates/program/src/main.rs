//! The Spacemesh standard wallet template.
#![no_main]
athena_vm::entrypoint!(main);

use parity_scale_codec::{Decode, Encode};
use wallet_common::{SendArguments, MethodId, SpawnArguments};

#[derive(Encode, Decode)]
pub struct Wallet {
    nonce: u64,
    balance: u64,
    owner: Pubkey,
}

impl Wallet {
    fn new(owner: Pubkey) -> Self {
        Wallet {
            nonce: 0,
            balance: 0,
            owner,
        }
    }

    fn send(&mut self, args: SendArguments) {
        // Send coins
        // Note: error checking happens inside the host
        unsafe {
            athena_vm::host::call(
                args.recipient.as_ptr(),
                std::ptr::null(),
                0,
                args.amount.to_le_bytes().as_ptr(),
            )
        }
    }
    fn proxy(&mut self, _args: Vec<u8>) {
        unimplemented!();
    }
    fn deploy(&mut self, _args: Vec<u8>) {
        unimplemented!();
    }
}

pub fn main() {
    // Read function selector and input to selected function.
    let method_id = athena_vm::io::read::<u8>();
    let encoded_method_args = athena_vm::io::read::<Vec<u8>>();

    // Template only implements a single method, spawn.
    // A spawned program implements the other methods.
    // This is the entrypoint for both.
    if method_id == MethodId::Spawn as u8 {
        // Template only implements spawn
        spawn(encoded_method_args);
    } else {
        // Instantiate the wallet and dispatch
        let spawn_args = athena_vm::io::read::<Vec<u8>>();
        let wallet = Wallet::decode(&mut &spawn_args[..]).unwrap();
        match method_id {
            MethodId::Send as u8 => {
                let send_arguments = SendArguments::decode(&mut &encoded_method_args[..]).unwrap();
                wallet.send(send_arguments);
            }
            MethodId::Proxy as u8 => {
                wallet.proxy(encoded_method_args);
            }
            MethodId::Deploy as u8 => {
                wallet.deploy(encoded_method_args);
            }
            _ => panic!("unsupported method")
        }
    };
}

fn spawn(args: Vec<u8>) {
    // Decode the arguments
    // This just makes sure they're valid
    let args = SpawnArguments::decode(&mut &args[..]).unwrap();

    // Spawn the wallet
    // Note: newly-created program address gets passed directly back from the VM
    // unsafe {
    //     athena_vm::host::spawn(args.as_ptr(), args.len());
    // }
}