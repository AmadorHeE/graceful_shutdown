# Graceful Shutdown

Graceful shutdown generally satisfies three minimum conditions:
- Stop the application from accepting new requests or messages from sources like HTTP, pub/sub systems, etc
- Wait for all ongoing requests to finish. If a request takes too long, respond with a graceful error.
- Release critical resources such as database connections, file locks, or network listeners.


# Background (Go internals)
Inside a Go process, there are 2 layers:
- Go runtime
- Your app
By default, the Go runtime intercepts signal first.

If a custom handler is registered, the flow is as follows:
```
Kernel  ---SIGTERM--->  Go runtime (listeners were registered)  --->  custom channel
```
If no custom handler is registered, then:
```
Kernel  ---SIGTERM--->  Go runtime (no listeners were registered)  --->  default handler  --->  process exits
```

## First iteration(from scratch)
Example of handling OS signals for graceful shutdown, after the first Ctrl+C, press it again to force exit.  
signal.Stop is used to stop receiving further signals after the first one is handled
```golang
import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	signalChan := make(chan os.Signal, 1) // A buffered channel with a capacity of 1 is a good choice for reliable signal handling
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	fmt.Println("Waiting for termination signal (SIGINT or SIGTERM)...")

	<-signalChan            // Block until a signal is received
	signal.Stop(signalChan) // Stop receiving any more signals
	close(signalChan)       // Close the channel to free resources
	time.Sleep(1 * time.Second)
	fmt.Println("Received termination signal, shutting down...")
}
```

## Second iteration(using signal.NotifyContext)
Starting with Go 1.16, you can simplify signal handling by using `signal.NotifyContext`, which ties signal handling to context cancellation:
```golang
import (
	"context"
	"os/signal"
	"syscall"
)

func main() {
	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM) // It returns a context that is canceled when one of the specified signals is received
	defer stop()

	// Setup tasks here

	<-rootCtx.Done() // Block until a signal is received
	stop()           // Stop receiving any more signals
}
```

# Third iteration(timeout awareness, stop accepting new requests, release critical resources)
This is the approach implemented in main.go.

It is important to know how long your application has to shut down after receiving a termination signal (`termination grace period`). Your shutdown logic must complete within this time.  
Assume the default is 30 seconds. It is a good practice to reserve about 20 percent of the time as a safety margin to avoid being killed before cleanup finishes. In this case, 25 seconds for shutting down and 5 for margin.
```
|-------------------------|-----|
     shut down period      margin
```

When using `net/http`, you can handle graceful shutdown by calling `http.Server.Shutdown` method. This method stops the server from accepting new connections and waits for all active requests to complete before shutting down idle connections.
Here is how it behaves:
- If a request is already in progress on an existing connection, the server will allow it to complete.
- If a client tries to make a new connection during shutdown, it will fail because the server’s listeners are already closed.

The health check endpoint (`readiness probe`) needs to be updated to reflect that the app is shutting down.
```golang
var isShuttingDown atomic.Bool

func readinessHandler(w http.ResponseWriter, _ *http.Request) {
	if isShuttingDown.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("shutting down"))
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}
```
We need to choose a timeout based on your shutdown budget
```golang
var timeout = 15 * time.Second

ctx, cancelFn := context.WithTimeout(context.Background(), timeout)
err := server.Shutdown(ctx)
```

A common issue is that handlers are not automatically aware when the server is shutting down.  
So, how can we notify our handlers that the server is shutting down? The answer is by using context.  

A common mistake is releasing critical resources as soon as the termination signal is received. At that point, your handlers and in-flight requests may still be using those resources. You should delay the resource cleanup until the shutdown timeout has passed or all requests are done.  
There are important cases where explicit cleanup is still necessary during shutdown

# Things to take in account
- All of this work around graceful shutdown won’t help if your functions do not respect `context cancellation`.
