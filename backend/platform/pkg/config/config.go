package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

func Get(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func MustGet(key string) string {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		log.Fatalf("config: required environment variable %q is not set", key)
	}
	return v
}

func GetInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
		log.Printf("config: %q is not a valid int, using default %d", key, def)
	}
	return def
}

func GetFloat(key string, def float64) float64 {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
		log.Printf("config: %q is not a valid float, using default %g", key, def)
	}
	return def
}

func GetBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "t", "true", "yes", "on":
			return true
		case "0", "f", "false", "no", "off":
			return false
		}
	}
	return def
}

func GetDuration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		log.Printf("config: %q is not a valid duration, using default %s", key, def)
	}
	return def
}

func GetSlice(key, def string) []string {
	raw := Get(key, def)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
