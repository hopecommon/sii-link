//go:build !darwin

package powerevent

import "context"

func Watch(context.Context) (Watcher, error) {
	return Watcher{}, ErrUnsupported
}
