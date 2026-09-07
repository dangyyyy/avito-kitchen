package config

import (
	"fmt"
	"os"
	"time"
)

type Platform struct {
	HTTPAddr    string
	DatabaseDSN string
	LogLevel    string
}

type Restaurant struct {
	HTTPAddr     string
	PlatformURL  string
	APIKey       string
	PollInterval time.Duration
	LogLevel     string
}

func LoadPlatform() (Platform, error) {
	dsn, err := required("DATABASE_DSN")
	if err != nil {
		return Platform{}, err
	}

	return Platform{
		HTTPAddr:    optional("HTTP_ADDR", ":8080"),
		DatabaseDSN: dsn,
		LogLevel:    optional("LOG_LEVEL", "info"),
	}, nil
}

func LoadRestaurant() (Restaurant, error) {
	platformURL, err := required("PLATFORM_URL")
	if err != nil {
		return Restaurant{}, err
	}

	apiKey, err := required("RESTAURANT_API_KEY")
	if err != nil {
		return Restaurant{}, err
	}

	pollInterval, err := duration("POLL_INTERVAL", 5*time.Second)
	if err != nil {
		return Restaurant{}, err
	}

	return Restaurant{
		HTTPAddr:     optional("HTTP_ADDR", ":8081"),
		PlatformURL:  platformURL,
		APIKey:       apiKey,
		PollInterval: pollInterval,
		LogLevel:     optional("LOG_LEVEL", "info"),
	}, nil
}

func required(key string) (string, error) {
	value := os.Getenv(key)
	if value == "" {
		return "", fmt.Errorf("environment variable %s is required", key)
	}

	return value, nil
}

func optional(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}

func duration(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}

	return parsed, nil
}
