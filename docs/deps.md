
## Dependencies Management

- We are using govendor for versioned deps
- We add all vendored libs to git (no reliance on external repos)
- Always add a dep using govendor
- For major deps we use wrappers. See logging.
- Danger: Do not add a dep from github directly using `go get` without using vendoring.

Add an external dep to /vendor from its latest upstream:
```
govendor fetch [go-package-name]
```

Add all external deps to /vendor:
```
govendor add +external
```



- [refs](https://github.com/kardianos/govendor/wiki/Govendor-CheatSheet)


