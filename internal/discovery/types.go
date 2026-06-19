// Package discovery scans the filesystem for docker-compose projects and
// extracts a normalised list of services for the dashboard UI.
package discovery

import "time"

// Service is a row in the "Services" card. It is intentionally a flat
// struct: the front-end renders one card per service from this record.
type Service struct {
	Project     string    `yaml:"-" json:"project"`
	Name        string    `yaml:"-" json:"name"`
	Image       string    `yaml:"-" json:"image"`
	Description string    `yaml:"-" json:"description"`
	URL         string    `yaml:"-" json:"url"`
	Ports       []string  `yaml:"-" json:"ports"`
	Labels      []string  `yaml:"-" json:"labels"`
	ComposeFile string    `yaml:"-" json:"composeFile"`
	Discovered  time.Time `yaml:"-" json:"discovered"`
}

// Result is the full output of one scanner pass.
type Result struct {
	Services []Service `json:"services"`
	Errors   []string  `json:"errors,omitempty"`
}
