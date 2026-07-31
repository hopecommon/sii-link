package powerevent

import "errors"

var ErrUnsupported = errors.New("system power events are unsupported on this platform")

type Watcher struct {
	Events <-chan struct{}
	Done   <-chan error
}
