package main

import (
	"encoding/json"
	"os"
)

/// State represents persisted agent state.
type State struct {
	Services map[string]bool `json:"services"`
}

/// LoadState loads state from disk.
func LoadState(path string) (*State, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return &State{Services: map[string]bool{}}, nil
	}

	var s State
	err = json.Unmarshal(b, &s)
	if s.Services == nil {
		s.Services = map[string]bool{}
	}
	return &s, err
}

/// SaveState saves state to disk.
func SaveState(path string, s *State) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}
