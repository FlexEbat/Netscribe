package main

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"syscall"
)

// maxPortSearch is how many ports above a busy one the automatic search tries.
const maxPortSearch = 100

// bindOptions says what bindListener does when the port is taken.
type bindOptions struct {
	// auto takes the next free port without asking.
	auto bool
	// ask reads one line from the person at the terminal. It is nil when there is no
	// terminal, for example under systemd or in a container, and then nobody is asked.
	ask func(prompt string) (string, error)
	// warn reports a port problem to the person who started the server.
	warn func(msg string, args ...any)
}

func (o bindOptions) warning(msg string, args ...any) {
	if o.warn != nil {
		o.warn(msg, args...)
	}
}

// bindListener opens the TCP listener of the panel. When the port is already in use it
// warns and then follows the options: pick the next free port, ask for another port, or
// fail with a message that says how to choose one. It runs before any worker starts, so
// a taken port cannot leave scans running behind a server that never came up.
func bindListener(addr string, o bindOptions) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		return ln, nil
	}
	if !errors.Is(err, syscall.EADDRINUSE) {
		return nil, fmt.Errorf("listen on %s: %w", addr, err)
	}
	host, port, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		return nil, fmt.Errorf("listen on %s: %w", addr, err)
	}
	o.warning("port is already in use", "addr", addr)
	switch {
	case o.auto:
		return nextFreePort(host, port, o)
	case o.ask != nil:
		return chooseListener(host, port, o)
	default:
		return nil, fmt.Errorf("%s is already in use: stop the program that holds it, set another port in listen, or pass --listen or --auto-port", addr)
	}
}

// nextFreePort tries the ports above port on host and returns the first listener it opens.
func nextFreePort(host, port string, o bindOptions) (net.Listener, error) {
	start, err := strconv.Atoi(port)
	if err != nil {
		return nil, fmt.Errorf("port %q is not a number", port)
	}
	last := min(start+maxPortSearch, 65535)
	for next := start + 1; next <= last; next++ {
		addr := net.JoinHostPort(host, strconv.Itoa(next))
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			o.warning("using another port", "from", port, "to", next)
			return ln, nil
		}
		if !errors.Is(err, syscall.EADDRINUSE) {
			return nil, fmt.Errorf("listen on %s: %w", addr, err)
		}
	}
	return nil, fmt.Errorf("no free port from %d to %d on %q", start+1, last, host)
}

// chooseListener asks until the person names a port that can be opened, asks for the
// automatic search, or gives up with an empty answer.
func chooseListener(host, port string, o bindOptions) (net.Listener, error) {
	prompt := fmt.Sprintf("Port %s is in use. Enter another port, \"auto\" for the next free one, or press Enter to quit: ", port)
	for {
		reply, err := o.ask(prompt)
		if err != nil {
			return nil, fmt.Errorf("port %s is in use and no other port was chosen: %w", port, err)
		}
		reply = strings.TrimSpace(reply)
		if reply == "" {
			return nil, fmt.Errorf("port %s is in use and no other port was chosen", port)
		}
		if reply == "auto" {
			return nextFreePort(host, port, o)
		}
		n, convErr := strconv.Atoi(reply)
		if convErr != nil || n < 1 || n > 65535 {
			o.warning("not a port number from 1 to 65535", "input", reply)
			continue
		}
		addr := net.JoinHostPort(host, strconv.Itoa(n))
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			o.warning("cannot listen there", "addr", addr, "err", err)
			continue
		}
		o.warning("using another port", "from", port, "to", n)
		return ln, nil
	}
}
