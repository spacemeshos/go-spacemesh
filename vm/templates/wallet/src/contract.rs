//! The Spacemesh standard single-signature wallet template.

use athena_interface::Address;
use athena_vm_declare::{callable, template};
use athena_vm_sdk::wallet::{ProxyArguments, SpendArguments, WalletProgram};
use athena_vm_sdk::{call, spawn, Pubkey, VerifiableTemplate};
use parity_scale_codec::{Decode, Encode};

#[derive(Decode, Encode)]
struct Wallet {
    owner: Pubkey,
}

#[template]
impl WalletProgram for Wallet {
    #[callable]
    fn spawn(pubkey: Pubkey) -> Address {
        let wallet = Wallet { owner: pubkey };
        spawn(&wallet.encode())
    }

    #[callable]
    fn spend(&self, args: SpendArguments) {
        call(args.recipient, None, None, args.amount);
    }

    #[callable]
    fn proxy(&self, args: ProxyArguments) -> Vec<u8> {
        call(args.destination, args.args, args.method, args.amount)
    }

    #[callable]
    fn deploy(&self, code: Vec<u8>) -> Address {
        athena_vm_sdk::deploy(&code)
    }

    #[callable]
    fn max_spend(&self, args: SpendArguments) -> u64 {
        args.amount
    }
}

#[template]
impl VerifiableTemplate for Wallet {
    #[callable]
    fn verify(&self, tx: Vec<u8>, signature: [u8; 64]) -> bool {
        athena_vm_sdk::precompiles::ed25519::verify(&tx, &self.owner.0, &signature)
    }
}
