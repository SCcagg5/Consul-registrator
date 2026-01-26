package main

import (
	"log"
	"os"
)

/// envOr returns the value of an environment variable or a default value.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

/// requireEnv returns the value of a mandatory environment variable or exits.
func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("missing mandatory environment variable: %s", key)
	}
	return v
}

/// envBool returns true if an environment variable is set to a truthy value.
func envBool(key string) bool {
	v := os.Getenv(key)
	return v == "1" || v == "true" || v == "TRUE" || v == "yes"
}
