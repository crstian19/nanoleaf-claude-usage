// Package discover finds Nanoleaf controllers on the local network.
//
// It scans rather than using mDNS, which sounds backwards and is not. A
// Nanoleaf does advertise itself over mDNS, but reaching that advert needs a
// resolver running on the machine, and on the author's own Arch install
// avahi-daemon was not running at all: the browse returned nothing while the
// panels sat there answering on their API port. A scan needs nothing but a
// socket, and sweeping a /24 takes well under a second.
package discover

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// Port is the TCP port the Nanoleaf local API listens on.
const Port = 16021

const (
	// dialTimeout is how long to wait for a host to accept a connection.
	// A panel on the same wired or wireless network answers in a few
	// milliseconds; this is generous for a busy access point.
	dialTimeout = 400 * time.Millisecond

	// confirmTimeout is how long to wait for the HTTP reply that proves a
	// host is really a panel.
	confirmTimeout = 2 * time.Second

	// workers is how many hosts are probed at once. High, because each
	// probe is almost entirely waiting.
	workers = 128

	// smallestNetwork is the widest prefix worth sweeping. A /22 is 1022
	// hosts; anything wider is a container bridge or a corporate range
	// where a scan is neither quick nor welcome.
	smallestNetwork = 22
)

// ErrNoNetworks means the machine has no local IPv4 network worth scanning.
var ErrNoNetworks = errors.New("discover: no local IPv4 network to scan")

// Panel is a controller found on the network.
type Panel struct {
	// Host is the address to talk to it on.
	Host string
	// Name is its hostname, when reverse DNS gives one. Nanoleaf devices
	// answer to names like "Shapes-9B63.local", which is how a person
	// tells two of them apart.
	Name string
}

// Find sweeps the machine's own IPv4 networks for panel controllers.
//
// progress may be nil. When given, it is called as hosts are probed, so a
// caller can show how far along the sweep is.
func Find(ctx context.Context, progress func(done, total int)) ([]Panel, error) {
	hosts, err := localHosts()
	if err != nil {
		return nil, err
	}
	if len(hosts) == 0 {
		return nil, ErrNoNetworks
	}

	var (
		mu    sync.Mutex
		found []Panel
		done  int
	)

	work := make(chan string)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for host := range work {
				panel, ok := probe(ctx, host)

				mu.Lock()
				done++
				if ok {
					found = append(found, panel)
				}
				at := done
				mu.Unlock()

				if progress != nil {
					progress(at, len(hosts))
				}
			}
		}()
	}

	for _, h := range hosts {
		select {
		case <-ctx.Done():
			close(work)
			wg.Wait()
			return found, ctx.Err()
		case work <- h:
		}
	}
	close(work)
	wg.Wait()

	return found, nil
}

// probe reports whether a host is a Nanoleaf controller.
//
// Two steps, because an open port is not proof. Any device may listen on
// 16021; only a Nanoleaf answers its API path with 401. That reply needs no
// token and no pairing mode, which is what makes identification possible
// before the user has either.
func probe(ctx context.Context, host string) (Panel, bool) {
	addr := net.JoinHostPort(host, fmt.Sprint(Port))

	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return Panel{}, false
	}
	_ = conn.Close()

	if !confirm(ctx, addr) {
		return Panel{}, false
	}
	return Panel{Host: host, Name: reverseName(ctx, host)}, true
}

// confirm checks the API answers the way a Nanoleaf does.
func confirm(ctx context.Context, addr string) bool {
	reqCtx, cancel := context.WithTimeout(ctx, confirmTimeout)
	defer cancel()

	// A deliberately invalid token: the reply distinguishes the device,
	// and nothing here needs to succeed.
	url := "http://" + addr + "/api/v1/discover/"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}

	resp, err := (&http.Client{Timeout: confirmTimeout}).Do(req)
	if err != nil {
		return false
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
	}()

	return resp.StatusCode == http.StatusUnauthorized
}

// reverseName looks up a friendly name, returning "" when there is none.
func reverseName(ctx context.Context, host string) string {
	lookupCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	names, err := net.DefaultResolver.LookupAddr(lookupCtx, host)
	if err != nil || len(names) == 0 {
		return ""
	}
	// Trim the trailing dot a resolver returns.
	return trimDot(names[0])
}

func trimDot(s string) string {
	if len(s) > 0 && s[len(s)-1] == '.' {
		return s[:len(s)-1]
	}
	return s
}
