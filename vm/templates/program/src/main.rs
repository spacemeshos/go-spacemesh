//! The Spacemesh standard wallet template.
#![no_main]
athena_vm::entrypoint!(main);

use athena_vm_sdk::call;
use parity_scale_codec::{Decode, Encode};
use wallet_common::{MethodId, Pubkey, SendArguments, SpawnArguments};

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

    fn send(self, args: SendArguments) {
        // Send coins
        // Note: error checking happens inside the host
        call(args.recipient, None, args.amount);
    }
    fn proxy(self, _args: Vec<u8>) {
        unimplemented!();
    }
    fn deploy(self, _args: Vec<u8>) {
        unimplemented!();
    }
}

pub fn main() {
    // Read function selector and input to selected function.
    let method_id = athena_vm::io::read::<u8>();
    let encoded_method_args = athena_vm::io::read::<Vec<u8>>();

    // convert method_id to MethodId enum
    let method = match method_id {
        0 => MethodId::Spawn,
        1 => MethodId::Send,
        2 => MethodId::Proxy,
        3 => MethodId::Deploy,
        _ => panic!("unsupported method"),
    };

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
        match method {
            MethodId::Send => {
                let send_arguments = SendArguments::decode(&mut &encoded_method_args[..]).unwrap();
                wallet.send(send_arguments);
            }
            MethodId::Proxy => {
                wallet.proxy(encoded_method_args);
            }
            MethodId::Deploy => {
                wallet.deploy(encoded_method_args);
            }
            _ => panic!("unsupported method"),
        }
    };
}

fn spawn(args: Vec<u8>) {
    // Decode the arguments
    // This just makes sure they're valid
    let args = SpawnArguments::decode(&mut &args[..]).unwrap();
    Wallet::new(args.owner);

    // Spawn the wallet
    // Note: newly-created program address gets passed directly back from the VM
    // unsafe {
    //     athena_vm::host::spawn(args.as_ptr(), args.len());
    // }
}
