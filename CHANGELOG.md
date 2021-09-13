# ChangeLog

All notable changes to go-spacemesh are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.1] - 2021/9/13

### Added

- zip file now includes the node, smrepl and gpu support

### Fixed

- smeshing can now start after being stopped
- waiting before validating blocks from the future
- AccontMeshDataQuery returns an internal error
- Smesher coinbase update API endpoint
- solved "requested unknown layer hash"
- fix a memory leak in post


### Changed

- shorten API server startup time
- remove bottlenecks from connection send queue
