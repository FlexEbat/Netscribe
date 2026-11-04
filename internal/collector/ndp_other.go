//go:build !linux

package collector

import "context"

func readNeighbours(context.Context) ([]byte, error) { return nil, errNoNDP }

func echoAllNodes(context.Context, []string) error { return errNoNDP }
