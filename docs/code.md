## Idiomatic GO
- We use [Go 1.9.2 or later](https://golang.org/dl/)
- We strive to reach Idiomatic GO by avoiding the Go anti-patterns discussed [here](https://www.youtube.com/watch?v=ltqV6pDKZD8)
- Note sure if a method should be accessed by pointer or value? read this: https://golang.org/doc/faq#methods_on_values_or_pointers
- Please read [go code review](https://github.com/golang/go/wiki/CodeReviewComments), and [effective go](https://golang.org/doc/effective_go.html)
- We strive to only use channels for all concurrent patterns and not mutexes or locks.
- Please gofmt the project to lint your code before checking in code changes and new code.
- When working on features or hot-fixes, consider using [gitflow git extensions](https://github.com/nvie/gitflow). 