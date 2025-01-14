fn main() {
    #[cfg(feature = "unittest")]
    athena_builder::build::build_program(".");
}
