# Coding guide

This project is written in Go and adheres to the accepted coding styles and practices defined in [Effective Go](https://golang.org/doc/effective_go.html) and [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments). 

## Static code analysis

Linting is done by using https://github.com/golangci/golangci-lint with configuration defined in the root of the project.

## Testing

It is advisable to use a separate test package to write tests (e.g. with the `package somepkg_test` namespace). This allows explicit testing on exported behaviour of the package, not the internal implementation (also known as black-box-testing). In need to export only to testing package, `export_test.go` file should be used. Testing internal implementations is discouraged, but not forbidden. It is preferred to treat exported package behaviours as units that need to be tested.

Executing `make test` should pass without any warnings or errors.

## Packages

Go packages with the spacemesh project should have a single and well defined responsibility, clear exposed API and tests that cover expected behaviour. To ensure better modularity of the codebase, every package should be treated as a library that provides a specific functionality and that can be used as a module in other applications. Every package should have a well written godoc page which should be used as the entry point for understanding the package's responsibility. The same as using any other third-party package. If the package godoc is not clear and requires looking at the code to understand behaviour that it provides, documentation should be improved.

## Concurrency

For every goroutine that is created, one must define how and when it terminates.

Every channel must have an owning goroutine. That goroutine is the only one that can send to the channel, close it or transfer ownership to another goroutine. Wherever is possible a readonly or writeonly channels should be used.

Usage of the `errgroup` library is highly encouraged instead of `sync.WaitGroup`, both for error propagation and for a cleaner group initialization.

## Error propagation, annotation and handling

Errors must be propagated by the caller function. Error should not be logged and passed at the same time. It is up to the next caller function to decide what should be done in case of an error (e.g. execute specific logic, log, or continue bubbling the error up the stack).

Any returned error from any function or method should be annotated either with `fmt.Errorf` using the `%w` verb or with custom error type that has `Unwrap() error` method. This approach has the advantage of resulting in an error message that is equivalent to a stack trace, since errors are repeatedly wrapped while being passed up the call stack until their eventual logging.

Explicit `error` values that are constructed with `errors.New` may have a message prefixed with the package name and a colon character, if it would improve the message clarity.

Comparison of error message should always use the `errors.Is` or `errors.As` methods as applicable. In case multiple error paths are possible, a `switch` may be used to reduce nesting.

## Logging Guidelines

This part of the document describes the basic principles of logging which should be followed when working with the code base.

Following these principles will not only make the logging output more coherent, but can significantly reduce time spent searching for bugs and make it easier for node operators to navigate the state of their actively running node.

### Logging libraries

In the codebase one can find usage of the `zap` library as well as usage of the `log` package which can be found at the root of the project. 

Currently, use of the own `log` package is discouraged in favor of the usage of `zap` library.

### Log Levels

The log messages are divided into four basic levels:

- `Error` - Errors in the node. Although the node operation may continue, the error indicates that it should be addressed.
- `Warning` - Warnings should be checked in case the problem recurs to avoid potential damage.
- `Info` - Informative messages useful for node operators that do not indicate any fault or error.
- `Debug` - Information concerning program logic decisions, diagnostic information, internal state, etc. which is primarily useful for developers.

### Log Messages

The key-value pairs (after a log message) should add more context to log messages. Keys are quite flexible and can contain more or less any string value. For consistency with existing code, there are a few conventions that should be followed:

- Make your keys human-readable and be specific as much as possible (choose "peer_address" instead of "address" or "peer", "batch_id" instead of "id" or "batch", etc.)
- Be consistent throughout the code base.
- Use lower case for simple keys and lower_snake_case for more complex keys.

Although key names are mostly unrestricted, it is generally a good idea to stick to printable ASCII characters, or at least to match the general character set of your log lines.

`Error` and `Warning` log messages should provide enough information to identify the problem in the code base without the need to record the file name and line number alongside them, although this information can be added using the logging library.

If you are writing/changing logs, it is recommended to look at the output. Answer questions like: is this what you expected? Is anything missing? Are they understandable? Do you need more/less context?

### Users

Two types of application users are identified: node operators and developers. Node operators should not be presented with confusing technical implementation details in log messages, but only with meaningful information regarding the functionality of the application in the form of meaningful statements. Developers are the users who know the implementation details and the technical details will help them in debugging the problematic state of the application.

In general, the goal for developers is to create log messages that are easy to understand, analyze, and reference code so they can troubleshoot issues quickly. In the case of node operators, log messages should be concise and provide all the necessary information without unnecessary details.

That is, the same problem event can have two log lines, but with different severity. This is the case for a combination of `Error/Debug` or `Warning/Debug`, where `Error` or `Warning` is for the node operator and `Debug` is for the developer to help them investigate the problem.

> The `Error` level should be used with caution when it comes to node operators. It is recommended to log application code errors at the `Error` level if they require node operator intervention or if the node cannot continue its normal operation (unrecoverable errors, from which the application should initiate a shutdown sequence). Otherwise, recoverable (application code) errors should be logged at the `Debug` level. The `Debug` level should not be used to monitor the node, but metrics should be used for this purpose.

Log lines should not contain the following attributes (remove, mask, sanitize, hash, or encrypt the following):

- application source code
- session identification values
- access tokens
- passwords
- database connection strings
- encryption keys and other primary secrets and sensitive information

### Recommendations

Some useful logging recommendations for cleaner output:

- Error - Error log messages are meant for regular users to be informed about the event that is happening which is not expected or may require intervention from the user for application to function properly. Log messages should be written as a statement, e.g. *"unable to connect to peer", "peer_address", r12345* and should not reveal technical details about internal implementation, such are package name, variable name or internal variable state. It should contain information only relevant to the user operating the application, concepts that can be operated on using exposed application interfaces.
- Warning - Warning log messages are also meant for regular users, as Error log messages, but are just informative about the state of the application that it can handle itself, by retry mechanism, for example. They should have the same form as the Error log messages.
- Info - Info log messages are informing users about the changes that are affecting the application state in the expected manner, or presenting the user the information that can be used to operate the application, such as *"node has successfully started" "listening_address", 12345*.
- Debug - Debug log messages are meant for developers to identify the problem that has happened. They should contain as much as possible internal technical information and applications state to be able to reproduce and debug the issue.
- Do not log a function entry unless it is significant or unless it is logged at the debug level.
- Avoid logging from within loops unless it is absolutely necessary.
- Truncate or summarize large messages or files in some way that will be useful for debugging.
- Avoid errors that are not really errors and can confuse the node operator. This sometimes happens when an application error is part of a successful execution flow.
- Do not log the same or similar errors repeatedly. It can quickly clutter the log and hide the real cause. The frequency of error types is best addressed by monitoring, and logs need to capture details for only some of these errors.

## Commit Messages

For commit messages, follow [this guideline](https://www.conventionalcommits.org/en/v1.0.0/). Use reasonable length for the subject and body, ideally no longer than 72 characters. Use the imperative mood for subject lines.

## Handling database schema changes

go-spacemesh currently maintains 2 SQLite databases in the data folder: `state.sql` (state database) and `local.sql` (local database). It employs schema versioning for both databases, with a possibility to upgrade older versions of each database to the current schema version by means of running a series of migrations. Also, go-spacemesh tracks any schema drift (unexpected schema changes) in the databases.

When a database is first created, the corresponding schema file embedded in go-spacemesh executable is used to initialize it:
* `sql/statesql/schema/schema.sql` for `state.sql`
* `sql/localsql/schema/schema.sql` for `local.sql`
The schema file includes `PRAGMA user_version = ...` which sets the version of the database schema. The version of the schema is equal to the number of migrations defined for the corresponding database (`state.sql` or `local.sql`).

For an existing database, the `PRAGMA user_version` is checked against the expected version number. If the database's schema version is too new, go-spacemesh fails right away as an older go-spacemesh version cannot be expected to work with a database from a newer version. If the database version number is older than the expected version, go-spacemesh runs the necessary migration scripts embedded in go-spacemesh executable and updates `PRAGMA user_version = ...`. The migration scripts are located in the following folders:
* `sql/statesql/schema/migrations` for `state.sql`
* `sql/localsql/schema/migrations` for `local.sql`

Additionally, some migrations ("coded migrations") can be implemented in Go code, in which case they reside in `.go` files located in `sql/statesql` and `sql/localsql` packages, respectively. It is worth noting that old coded migrations can be removed at some point, rendering database versions that are *too* old unusable with newer go-spacemesh versions.

After all the migrations are run, go-spacemesh compares the schema of each database to the embedded schema scripts and if they differ, fails with an error message:
```
Error: open sqlite db schema drift detected (uri file:data/state.sql):
  (
        """
        ... // 82 identical lines
            PRIMARY KEY (layer, block)
        );
+       CREATE TABLE foo(id int);
        CREATE TABLE identities
        (
        ... // 66 identical lines
        """
  )
```

In this case, a table named `foo` has somehow appeared in the database, causing go-spacemesh to fail due to the schema drift. The possible reasons for schema drift can be the following:
* running an unreleased version of go-spacemesh using your data folder. The unreleased version may contain migrations that may be changed before the release happens
* manual changes in the database
* external SQLite tooling used on the database that adds some tables, indices etc.

In case if you want to run go-spacemesh with schema drift anyway, you can set `main.db-allow-schema-drift` to true. In this case, a warning with schema diff will be logged instead of failing.

The schema changes in go-spacemesh code should be always done by means of adding migrations. Let's for example create a new migration (use zero-padded N+1 instead of 0010 with N being the number of the last migration for the local db):

```console
$ echo 'CREATE TABLE foo(id int);' >sql/localsql/schema/migrations/0010_foo.sql
```

After that, we update the schema files
```console
$ make generate
$ # alternative: cd sql/localsql && go generate
$ git diff sql/localsql/schema/schema.sql
diff --git a/sql/localsql/schema/schema.sql b/sql/localsql/schema/schema.sql
index 02c44d3cc..ebcdf4278 100755
--- a/sql/localsql/schema/schema.sql
+++ b/sql/localsql/schema/schema.sql
@@ -1,4 +1,4 @@
-PRAGMA user_version = 9;
+PRAGMA user_version = 10;
 CREATE TABLE atx_sync_requests
 (
     epoch     INT NOT NULL,
@@ -24,6 +24,7 @@ CREATE TABLE "challenge"
     post_indices  VARCHAR,
     post_pow      UNSIGNED LONG INT
 , poet_proof_ref        CHAR(32), poet_proof_membership VARCHAR) WITHOUT ROWID;
+CREATE TABLE foo(id int);
 CREATE TABLE malfeasance_sync_state
 (
   id INT NOT NULL PRIMARY KEY,
```

Note that the changes include both the new table and an updated `PRAGMA user_version` line.
The changes in the schema file must be committed along with the migration we added.
```console
$ git add sql/localsql/schema/migrations/0010_foo.sql sql/localsql/schema.sql
$ git commit -m "sql: add a test migration"
```
