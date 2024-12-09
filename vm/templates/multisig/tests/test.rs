use std::error::Error;

use athena_interface::{Address, MethodSelector};
use athena_sdk::{AthenaStdin, ExecutionClient};
use athena_vm_sdk::Pubkey;
use parity_scale_codec::Encode;

use ed25519_dalek::ed25519::signature::Signer;
use rand::rngs::OsRng;

pub const PROGRAM: &[u8] = include_bytes!("../elf/multisig");
pub const ADDRESS_ALICE: Address = Address([1u8; 24]);

#[derive(Encode)]
struct SpawnArguments {
    required: u8,
    keys: Vec<Pubkey>,
}

#[derive(Clone)]
struct SigningKey {
    id: u8,
    key: ed25519_dalek::SigningKey,
}

fn spawn(required: u8, keys: Vec<Pubkey>) -> Result<(Address, Vec<u8>), Box<dyn Error>> {
    let mut stdin = AthenaStdin::new();
    let (state_w, state_r) = std::sync::mpsc::channel();
    let mut host = athena_interface::MockHostInterface::new();
    host.expect_spawn().returning_st(move |s| {
        state_w.send(s).unwrap();
        ADDRESS_ALICE
    });

    let args = SpawnArguments { required, keys };
    stdin.write_vec(args.encode());

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
    Ok((result.read(), state))
}

fn verify(state: Vec<u8>, keys: &[SigningKey]) -> bool {
    let tx = b"some really bad tx";

    let mut stdin = AthenaStdin::new();
    stdin.write_vec(state);
    stdin.write_vec(tx.as_slice().encode());

    for key in keys.iter() {
        let signature = key.key.sign(tx);
        stdin.write_vec(key.id.encode());
        stdin.write_vec(signature.to_bytes().encode());
    }

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

fn create_keys(count: u8) -> (Vec<SigningKey>, Vec<Pubkey>) {
    let keys: Vec<_> = (0..count)
        .map(|id| SigningKey {
            id,
            key: ed25519_dalek::SigningKey::generate(&mut OsRng),
        })
        .collect();
    let pubkeys = keys
        .iter()
        .map(|k| Pubkey(k.key.verifying_key().to_bytes()))
        .collect();

    (keys, pubkeys)
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

    let (address, state) = spawn(1, vec![Pubkey(signing_key.verifying_key().to_bytes())]).unwrap();

    assert_eq!(ADDRESS_ALICE, address);
    assert!(!state.is_empty());
}

#[test]
fn verify_with_all_keys() {
    setup_logger();
    let (keys, pubkeys) = create_keys(5);
    let (_, state) = spawn(keys.len() as u8, pubkeys).unwrap();

    let valid = verify(state, &keys);
    assert!(valid);
}

#[test]
fn verify_with_required_keys() {
    setup_logger();
    let (keys, pubkeys) = create_keys(5);
    let (_, state) = spawn(2, pubkeys).unwrap();

    let valid = verify(state.clone(), &[keys[0].clone(), keys[2].clone()]);
    assert!(valid);

    // signatures must be ordered by ID
    let valid = verify(state, &[keys[2].clone(), keys[0].clone()]);
    assert!(!valid);
}

#[test]
fn verify_fails_when_insufficient_keys() {
    setup_logger();
    let (keys, pubkeys) = create_keys(5);
    let (_, state) = spawn(5, pubkeys).unwrap();

    let valid = verify(state, &keys[..4]);
    assert!(!valid);
}

#[test]
fn verify_must_be_signed_with_different_keys() {
    setup_logger();
    let (keys, pubkeys) = create_keys(5);
    let (_, state) = spawn(2, pubkeys).unwrap();

    let valid = verify(state, &[keys[0].clone(), keys[0].clone()]);
    assert!(!valid);
}
