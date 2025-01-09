use athena_vm_sdk::Pubkey;
use parity_scale_codec::{Decode, Encode};

#[derive(Encode, Decode)]
pub struct SpawnArguments {
    pub required: u8,
    pub keys: Vec<Pubkey>,
}

#[derive(Encode, Decode)]
pub struct Signature {
    pub id: u8,
    pub sig: [u8; 64],
}
