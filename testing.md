## Testing Notes

### Code Coverage
Follow these steps to view tests code coverage for a package:
- cd to a package directory
- `go test ./.... -coverprofile=cover.out`
- `go tool cover -html=cover.out`
