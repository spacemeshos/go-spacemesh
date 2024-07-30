use athena_interface::{Address, Bytes32};
use parity_scale_codec::{Decode, Encode};

pub type Pubkey = Bytes32;

#[derive(Encode, Decode)]
pub struct SendArguments {
    recipient: Address,
    amount: u64,
}

#[derive(Encode, Decode)]
pub struct SpawnArguments {
    owner: Pubkey,
}

// The method selectors
pub enum MethodId {
    Spawn = 0,
    Send = 1,
    Proxy = 2,
    Deploy = 3,
}
