package postmortenHandler

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/systest/parameters"
	"go.uber.org/zap"
)

/*
how the post-morten debug handler works:
1. As soon the namespace is registered. The user has PostMortenConfirmationWindow
Minutes to access the post-morten debug handler and perform POST request to notify the handler that the namespace is being debugged.
That will extend the expiration time to PostMortenConfirmationWindow + PostMortenDebugDuration.
2. if not the namespace will be cleaned up.
3. The user performing the POST request to extend the expiration time to Now + the time passed on the request.
But that time cannot exceed PostMortenDebugMaxDuration.
4. The user can perform a GET request to check the status of the namespace.
5. The user can perform a DELETE request to clean up the namespace.
*/
type PostMortenDebugHandler struct {
	Namespace         string
	isBeingDebugged   bool
	expriationTime    int64
	createTime        int64
	maximumDuration   int64
	cleaningFunctions chan func()
	param             *parameters.Parameters
}

var (
	Starter          sync.Once
	server           *http.Server
	Log              *zap.SugaredLogger
	namespaces       = map[string]*PostMortenDebugHandler{}
	lazyRegistration = []map[string]func(){}

	// input parameters
	PosTMortenDebug = parameters.Bool(
		"post-morten-debug", "if true post-morten debug is allowed",
	)
	PostMortenConfirmationWindow = parameters.Int(
		"post-morten-confirmation-window", "time in minutes of the post-morten confirmation window. ", 30,
	)
	PostMortenDebugDuration = parameters.Int(
		"post-morten-debug-duration", "time in minutes of the post-morten debug duration. ", 120,
	)
	postMortenDebugMaxDuration = parameters.Int(
		"post-morten-debug-max-duration", "time in minutes of the post-morten debug max duration. ", 600,
	)
	postMortenDebugHandlerPort = parameters.Int(
		"post-morten-debug-handler-port", "post-morten debug handler port. ", 8080,
	)

	// RegistrationLock
	RegistrationLock sync.Mutex
)

// LazyFunctionActor - Because we cannot garantee if the cleanup function executed before the server started.
// Example: a bad configuration that will cause the server to not start.
func LazyFunctionActor(f func()) func() {
	return func() {
		if server == nil {
			f()
		}
	}
}

// Register Namespace
func RegisterNamespace(namespace string, f func(), param *parameters.Parameters) {
	RegistrationLock.Lock()
	defer RegistrationLock.Unlock()

	if server == nil {
		lazyRegistration = append(lazyRegistration, map[string]func(){namespace: f})
		return
	}

	if _, exist := namespaces[namespace]; exist {
		Log.Warnf("namespace %s already registered", namespace)
		cleanupFunctions := namespaces[namespace].cleaningFunctions

		if f != nil {
			cleanupFunctions <- f
		}

		return
	}

	namespaces[namespace] = &PostMortenDebugHandler{
		Namespace:         namespace,
		isBeingDebugged:   false,
		createTime:        time.Now().Unix(),
		maximumDuration:   time.Now().Unix() + int64(postMortenDebugMaxDuration.Get(param)*time.Now().Minute()),
		expriationTime:    time.Now().Unix() + int64(PostMortenConfirmationWindow.Get(param)*time.Now().Minute()),
		cleaningFunctions: make(chan func()),
	}

	cleanupFunctions := namespaces[namespace].cleaningFunctions

	if f != nil {
		cleanupFunctions <- f
	}

}

// Actor
func Actor() {
	time.Sleep(1 * time.Second)
	wg := sync.WaitGroup{}

	for len(namespaces) > 0 {

		for ns, handler := range namespaces {
			if time.Now().Unix() > handler.expriationTime {
				Log.Infof("namespace %s expired", ns)
				wg.Add(1)
				go func() {
					defer wg.Done()
					for f := range handler.cleaningFunctions {
						f()
					}
				}()
				delete(namespaces, ns)
			}

		}
		wg.Wait()
	}
	Log.Infof("all namespaces expired")

}

// StartServer starts the postmorten server
func StartServer(p *parameters.Parameters, l *zap.SugaredLogger) {
	if PosTMortenDebug.Get(p) {
		Starter.Do(func() {
			Log = l
			port := fmt.Sprintf(":%d", postMortenDebugHandlerPort.Get(p))
			server = &http.Server{Addr: port}

			for _, reg := range lazyRegistration {
				for namespace, f := range reg {
					RegisterNamespace(namespace, f, p)
				}
			}

			go func() {
				if err := server.ListenAndServe(); err != nil {
					panic(err)
				}
			}()
			defer server.Close()
			Actor()

		})
	}
}
