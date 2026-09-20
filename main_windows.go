package main

import (
	"runtime"
)

func main() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	setProcessDPIAware()
	app := newApplication()
	if err := app.run(); err != nil {
		messageBox(0, err.Error(), appName, mbIconError)
	}
}
