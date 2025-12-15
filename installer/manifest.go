// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

package installer

// Manifest represents a GitHub App manifest.
type Manifest struct {
	Name           string            `json:"name,omitempty"`
	URL            string            `json:"url"`
	HookAttributes HookAttributes    `json:"hook_attributes"`
	RedirectURL    string            `json:"redirect_url"`
	Public         bool              `json:"public"`
	DefaultPerms   map[string]string `json:"default_permissions"`
	DefaultEvents  []string          `json:"default_events"`
}

// HookAttributes configures the webhook for the GitHub App.
type HookAttributes struct {
	URL    string `json:"url"`
	Active bool   `json:"active"`
}

// Clone returns a deep copy of the manifest.
func (m *Manifest) Clone() *Manifest {
	if m == nil {
		return nil
	}

	clone := &Manifest{
		Name:           m.Name,
		URL:            m.URL,
		RedirectURL:    m.RedirectURL,
		Public:         m.Public,
		HookAttributes: m.HookAttributes,
	}

	if m.DefaultPerms != nil {
		clone.DefaultPerms = make(map[string]string, len(m.DefaultPerms))
		for k, v := range m.DefaultPerms {
			clone.DefaultPerms[k] = v
		}
	}

	if m.DefaultEvents != nil {
		clone.DefaultEvents = make([]string, len(m.DefaultEvents))
		copy(clone.DefaultEvents, m.DefaultEvents)
	}

	return clone
}
