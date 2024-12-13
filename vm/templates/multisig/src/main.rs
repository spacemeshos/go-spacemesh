#![cfg_attr(target_os = "zkvm", no_main)]

#[cfg(target_os = "zkvm")]
mod contract;

#[cfg(target_os = "zkvm")]
use athena_vm::entrypoint;
#[cfg(target_os = "zkvm")]
athena_vm::entrypoint!();

#[cfg(not(target_os = "zkvm"))]
pub fn main() {}
