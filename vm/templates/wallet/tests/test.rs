use std::error::Error;

use athena_interface::{
    payload::Payload, Address, AthenaContext, ExecutionResult, MethodSelector, StatusCode,
};
use athena_sdk::{host::MockHostInterface, AthenaStdin, ExecutionClient};
use athena_vm_sdk::{
    wallet::{ProxyArguments, SpendArguments},
    Pubkey,
};
use parity_scale_codec::{Decode, Encode, IoReader};

use ed25519_dalek::{ed25519::signature::Signer, SigningKey};
use rand::rngs::OsRng;

pub const PROGRAM: &[u8] = include_bytes!("../elf/wallet");

fn spawn(pubkey: Pubkey) -> Result<(Address, Vec<u8>), Box<dyn Error>> {
    let address_alice = Address::from([1u8; 24]);
    let mut stdin = AthenaStdin::new();
    let (state_w, state_r) = std::sync::mpsc::channel();
    let mut host = MockHostInterface::new();
    host.expect_spawn().returning_st(move |s| {
        state_w.send(s).unwrap();
        address_alice
    });

    stdin.write_vec(pubkey.encode());

    let method_selector = MethodSelector::from("athexp_spawn");

    let client = ExecutionClient::new();
    let (mut result, _) = client.execute_function(
        PROGRAM,
        &method_selector,
        stdin,
        Some(&mut host),
        None,
        None,
    )?;

    let state = state_r.recv().unwrap();
    let address = Address(result.read());
    assert_eq!(address_alice, address);
    Ok((address, state))
}

fn verify(state: Vec<u8>, key: &SigningKey) -> bool {
    let tx = b"some really bad tx";

    let mut stdin = AthenaStdin::new();
    stdin.write_vec(state);
    stdin.write_vec(tx.as_slice().encode());

    let signature = key.sign(tx);
    stdin.write_vec(signature.to_bytes().encode());

    let result = ExecutionClient::new().execute_function(
        PROGRAM,
        &MethodSelector::from("athexp_verify"),
        stdin,
        None,
        Some(100000),
        None,
    );
    let (mut result, _) = result.unwrap();
    result.read::<bool>()
}

fn setup_logger() {
    let _ = tracing_subscriber::fmt()
        .with_test_writer()
        .with_env_filter(tracing_subscriber::EnvFilter::from_default_env())
        .try_init();
}

#[test]
fn spawning() {
    setup_logger();
    let signing_key = ed25519_dalek::SigningKey::generate(&mut OsRng);
    let owner = Pubkey(signing_key.verifying_key().to_bytes());
    let (_, state) = spawn(owner).unwrap();
    assert_eq!(&state, owner.0.as_slice());
}

#[test]
fn proxying() {
    setup_logger();

    let signing_key = ed25519_dalek::SigningKey::generate(&mut OsRng);
    let owner = Pubkey(signing_key.verifying_key().to_bytes());
    let (address, state) = spawn(owner).unwrap();

    let mut stdin = AthenaStdin::new();
    stdin.write_vec(state);
    let args = ProxyArguments {
        destination: Address::from([8u8; 24]),
        method: Some(MethodSelector::from("call_me")),
        args: Some(vec![1, 2, 3, 4]),
        amount: 981235,
    };
    stdin.write_vec(args.encode());

    let mut host = MockHostInterface::new();
    host.expect_call().returning_st(move |msg| {
        assert_eq!(1, msg.depth);
        assert_eq!(address, msg.sender);
        assert_eq!(args.destination, msg.recipient);
        let expected_input = Payload {
            selector: args.method.clone(),
            input: args.args.clone().unwrap(),
        };
        assert_eq!(Some(expected_input.encode()), msg.input_data);

        ExecutionResult {
            output: Some(vec![6, 7, 8, 9]),
            status_code: StatusCode::Success,
            gas_left: 50_000,
        }
    });
    let result = ExecutionClient::new().execute_function(
        PROGRAM,
        &MethodSelector::from("athexp_proxy"),
        stdin,
        Some(&mut host),
        Some(100000),
        Some(AthenaContext::new(address, Address::default(), 0)),
    );
    let (result, _) = result.unwrap();
    assert_eq!(
        vec![6, 7, 8, 9],
        Vec::<u8>::decode(&mut IoReader(result.as_slice())).unwrap()
    );
}

#[test]
fn max_spend() {
    setup_logger();
    let signing_key = ed25519_dalek::SigningKey::generate(&mut OsRng);
    let owner = Pubkey(signing_key.verifying_key().to_bytes());
    let (_, state) = spawn(owner).unwrap();

    let mut stdin = AthenaStdin::new();
    stdin.write_vec(state);
    let args = SpendArguments {
        recipient: Address::from([8u8; 24]),
        amount: 981235,
    };
    stdin.write_vec(args.encode());

    let result = ExecutionClient::new().execute_function(
        PROGRAM,
        &MethodSelector::from("athexp_max_spend"),
        stdin,
        None,
        Some(100000),
        None,
    );
    let (mut result, _) = result.unwrap();
    assert_eq!(args.amount, result.read::<u64>());
}

#[test]
fn verifying() {
    setup_logger();
    let signing_key = ed25519_dalek::SigningKey::generate(&mut OsRng);
    let owner = Pubkey(signing_key.verifying_key().to_bytes());
    let (_, state) = spawn(owner).unwrap();

    let valid = verify(state, &signing_key);
    assert!(valid);
}

#[test]
fn verifying_fails_for_invalid_signature() {
    setup_logger();
    let (_, state) = spawn(Pubkey::default()).unwrap();

    let valid = verify(state, &ed25519_dalek::SigningKey::generate(&mut OsRng));
    assert!(!valid);
}
