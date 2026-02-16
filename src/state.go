package main

import (
	"encoding/json"
	"os"
)

type State struct {
	Services      map[string]bool   `json:"services"`
	ServiceHashes map[string]string `json:"service_hashes"`
}

func LoadState(path string) (*State, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return &State{
			Services:      map[string]bool{},
			ServiceHashes: map[string]string{},
		}, nil
	}

	var s State
	err = json.Unmarshal(b, &s)

	if s.Services == nil {
		s.Services = map[string]bool{}
	}
	if s.ServiceHashes == nil {
		s.ServiceHashes = map[string]string{}
	}

	return &s, err
}

func SaveState(path string, s *State) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}
