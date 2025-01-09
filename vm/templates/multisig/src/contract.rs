//! The Spacemesh standard multi-signature wallet template.

use athena_interface::Address;
use athena_vm_declare::{callable, template};
use athena_vm_sdk::wallet::SpendArguments;
use athena_vm_sdk::{call, spawn, Pubkey};
use parity_scale_codec::{Decode, Encode, IoReader};

#[derive(Encode, Decode)]
struct Contract {
    required: u8,
    keys: Vec<Pubkey>,
}

#[template]
impl Contract {
    #[callable]
    fn spawn(args: multisig::SpawnArguments) -> Address {
        let wallet = Contract {
            required: args.required,
            keys: args.keys,
        };
        let serialized = wallet.encode();
        spawn(&serialized)
    }

    #[callable]
    fn spend(&self, args: SpendArguments) {
        call(args.recipient, None, None, args.amount);
    }

    #[callable]
    fn deploy(&self, code: Vec<u8>) -> Address {
        athena_vm_sdk::deploy(&code)
    }

    #[callable]
    fn max_spend(&self, args: SpendArguments) -> u64 {
        args.amount
    }

    #[callable]
    fn verify() -> bool {
        let mut io = IoReader(athena_vm::io::Io::default());
        let state = Contract::decode(&mut io).unwrap();
        let tx = Vec::<u8>::decode(&mut io).unwrap();

        let mut last_id = None;
        for _ in 0..state.required {
            let sig = if let Ok(s) = multisig::Signature::decode(&mut io) {
                s
            } else {
                return false;
            };

            if state.keys.len() < sig.id as usize {
                return false;
            }
            if let Some(last) = last_id {
                if sig.id <= last {
                    return false;
                }
            }
            last_id = Some(sig.id);
            let pubkey = &state.keys[sig.id as usize];

            if !athena_vm_sdk::precompiles::ed25519::verify(&tx, &pubkey.0, &sig.sig) {
                return false;
            }
        }
        return true;
    }
}
