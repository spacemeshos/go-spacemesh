## Testing Notes

### Running all tests

`make test`

### Generating a Code Coverage Report
`make cover`

### Travis CI for cloud building & testing
- Travis builds run all tests
- A travis build is triggered for each PR
- CI will fail if code is not proparly go formatted or one or more go vet check is failing.
- https://travis-ci.org/spacemeshos/go-spacemesh
