## Testing

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
