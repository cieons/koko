package devicemux

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"golang.org/x/sys/windows"
)

// watchRoutes watches the device's routes and unicast addresses through
// Windows' notifications (NotifyRouteChange2 and
// NotifyUnicastIpAddressChange), until ctx ends, when it cancels both in a
// goroutine wg tracks.
func watchRoutes(ctx context.Context, wg *sync.WaitGroup) (<-chan struct{}, error) {
	changed := make(chan struct{}, 1)
	cancel, err := notifyRouteChanges(changed)
	if err != nil {
		return nil, err
	}
	wg.Go(func() {
		<-ctx.Done()
		_ = cancel() // the Device Mux is stopping: a registration left over only wakes no one
	})
	return changed, nil
}

// notifyRouteChanges registers for Windows' notifications of a change of
// the routes and of the unicast addresses, of IPv4 and IPv6, each of which
// sends to changed, at most one waiting, until cancel cancels both.
func notifyRouteChanges(changed chan struct{}) (cancel func() error, err error) {
	routeWatchers.add(changed)
	var routes, addresses windows.Handle
	err = windows.NotifyRouteChange2(windows.AF_UNSPEC, routeCallback(), nil, false, &routes)
	if err != nil {
		routeWatchers.remove(changed)
		return nil, fmt.Errorf("register for route changes: %w", err)
	}
	err = windows.NotifyUnicastIpAddressChange(windows.AF_UNSPEC, routeCallback(), nil, false, &addresses)
	if err != nil {
		_ = windows.CancelMibChangeNotify2(routes)
		routeWatchers.remove(changed)
		return nil, fmt.Errorf("register for address changes: %w", err)
	}
	return func() error {
		// Never under routeWatchers' lock: a cancel waits for a callback
		// under way, which takes it.
		err := errors.Join(windows.CancelMibChangeNotify2(routes), windows.CancelMibChangeNotify2(addresses))
		routeWatchers.remove(changed)
		if err != nil {
			return fmt.Errorf("cancel the route and address notifications: %w", err)
		}
		return nil
	}, nil
}

// routeCallback is the callback Windows calls with each notification, made
// once for the process: Go never frees one, and a test process runs many
// Device Muxes. It is given no context, so it wakes every watcher; each
// notification is a change of the same machine's network.
var routeCallback = sync.OnceValue(func() uintptr {
	// Its arguments, the caller's context, the row and the kind of change,
	// say nothing it needs.
	return windows.NewCallback(func(_, _, _ uintptr) uintptr {
		routeWatchers.notify()
		return 0
	})
})

// routeWatchers are the channels the notifications wake.
var routeWatchers watchers

type watchers struct {
	mu  sync.Mutex
	set map[chan struct{}]struct{}
}

func (w *watchers) add(c chan struct{}) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.set == nil {
		w.set = map[chan struct{}]struct{}{}
	}
	w.set[c] = struct{}{}
}

func (w *watchers) remove(c chan struct{}) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.set, c)
}

// notify sends each watcher a change, unless one is waiting already.
func (w *watchers) notify() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for c := range w.set {
		select {
		case c <- struct{}{}:
		default:
		}
	}
}
