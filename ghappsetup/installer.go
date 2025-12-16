// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

package ghappsetup

import (
	"net/http"

	"github.com/cruxstack/github-app-setup-go/installer"
)

// InstallerHandler creates an installer http.Handler with the Runtime's store
// and reload callback pre-configured. This is a convenience method that
// simplifies the common case of wiring up the installer with the runtime.
//
// The returned handler should be mounted at /setup, /setup/, /callback, and /
// paths. For example:
//
//	installerHandler, err := runtime.InstallerHandler(installer.Config{
//	    Manifest:       manifest,
//	    AppDisplayName: "My App",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	mux.Handle("/setup", installerHandler)
//	mux.Handle("/setup/", installerHandler)
//	mux.Handle("/callback", installerHandler)
//	mux.Handle("/", installerHandler)
//
// The Config.Store and Config.OnReloadNeeded fields are automatically set
// by this method and should not be provided in the input config.
func (r *Runtime) InstallerHandler(cfg installer.Config) (http.Handler, error) {
	// Set store and reload callback automatically
	cfg.Store = r.store
	cfg.OnReloadNeeded = r.ReloadCallback()

	return installer.New(cfg)
}
