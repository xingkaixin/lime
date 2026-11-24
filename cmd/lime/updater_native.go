//go:build !js

package main

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
)

var updaterEnabled bool

func initUpdater() {
	mygo.Use(native.Plugin)
	updaterEnabled = true
}

func updaterMenuItem() *mygo.MenuItem {
	// Skip in tests where main() never initialized the plugin or event loop.
	if !updaterEnabled {
		return nil
	}
	return updater.MenuItem()
}
