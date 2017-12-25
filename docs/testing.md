## Testing

- We strive to get to 100% tests code coverage to ensure high-quality.
- Tests are the the best code documentation as they demonstrate how to use apis (functions, types, flows between types to achive a goal)
- The level of testing in an open source project is an important factor for serious contribution candidates.
- As a rule - no new feature, package or function should be push to `develop` unless it includes at least basic sanity tests.
- More complex packages should have additional tests. There's never enough of them.
- New contributors are encouraged to start by writting tests before adding new features.
- We are using [go tests](https://golang.org/pkg/testing/) for all tests.
- All code modules should have tests.
- All new functions, major types and modules should have tests.
- All tests must pass before pushing new code ot master.
- You may use GoLang GO TEST configs for testing.
- To test a package such as node:

Run all test:
```
make test
```

Run tests in a test folder

```
cd app/tests
go test
```

- Run all tests from proj root folder:
```
go test ./...
```

- Run all tests via govendor
```
govendor test +local
```

- Race detection:

```
go run -race main.go
```

- Code Coverage
```
go test ./... -cover
```

### CI
- TBD - travis builds and tests

### Custom node tests
- Node is designed to be independent of app so you should be able to create custom p2p tests.
See swarm_test.go
