//go:build windows

package router

import "os"

func shutdownSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}
