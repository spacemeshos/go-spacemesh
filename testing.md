mp## Testing Notes

### Running all tests

`make test`

### Generating a Code Coverage Report
`make cover`

### Travis CI for cloud building & testing
- Travis builds run all tests
- A travis build is triggered for each PR
- CI will fail if code is not proparly go formatted or one or more go vet check is failing.
- https://travis-ci.org/spacemeshos/go-spacemesh


### Checking Out and Testing Pull Requests

- Open up the .git/config file and add a new line under [remote "origin"]:

- ```fetch = +refs/pull/*/head:refs/pull/origin/*```

Now you can fetch and checkout any pull request so that you can test them:

- Fetch all pull request branches
```git fetch origin```

- Checkout out a given pull request branch based on its number
```git checkout -b 999 pull/origin/999```


### The Table-driven pattern for tests is good!
- See page 13 and on => https://leanpub.com/productiongo