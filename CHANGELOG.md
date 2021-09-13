# ChangeLog

All notable changes to go-spacemesh are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.1] - 2021/9/13

### Added

- Zip file now includes the node, smrepl and gpu support

### Fixed

- Smeshing can now start after being stopped
- Waiting before validating blocks from the future
- AccountMeshDataQuery returns an internal error
- Smesher coinbase update API endpoint
- Solved "requested unknown layer hash"
- Fix a memory leak in post


### Changed

- Shorten API server startup time
- Remove bottlenecks from connection send queue
