## Testing

- We strive to have at least one unit test per function and per package.
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

```
cd node/tests
go test
```

- Run all tests from proj root folder:
```
go test ./...
```
### CI
- TBD - travis builds and tests
