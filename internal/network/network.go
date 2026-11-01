// Package network gives the app's HTTP clients their one setting that the
// listener owns: the proxy their desktop sends its other traffic through.
//
// Go reaches the network directly unless a client is told otherwise, so an
// app that ignores the system's proxy fails where the listener's browser
// works. Every client the app talks to a service with is made here, so that
// none of them forgets it.
package network

import (
	"net/http"
	"time"

	"github.com/egoist/mygo/netproxy"
)

// Transport goes where the desktop's proxy settings send a request, with
// their bypass list and setup script, and follows the environment first
// when it names one. MyGo reads and keeps the desktop's answer; it applies
// to requests that follow a change of the settings without a restart.
func Transport() *http.Transport { return netproxy.Transport() }

// Client is an HTTP client that takes timeout for a request's whole
// lifetime, and reaches the network as the desktop does. A timeout of zero
// waits as long as the request takes.
func Client(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: Transport()}
}
