## Dependencies Management

- We use the full github path for local packages
- We are using govendor for versioned deps.
- We add all vendored libs to the repo (no reliance on external repos).
- Always add a dep using govendor.
- For major deps we use wrappers. See logging.
- Danger: Do not add a dep from github directly using `go get` without using vendoring.

Adding an external dep to /vendor from its latest upstream:
```
govendor fetch [go-package-name]
```

Adding all external deps to /vendor:
```
govendor add +external
```

Check that all deps are vendored. e.g. do not come from a local go path:
```
govendor list
```

Please read the [cheat-sheet](https://github.com/kardianos/govendor/wiki/Govendor-CheatSheet)
All deps should be local (l) or vendored (v)

