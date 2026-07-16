package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DatabaseURL             string
	ServerPort              string
	EncryptionKey           string
	ScheduleAM              string
	SchedulePM              string
	SyncOnStartup           bool
	SyncStartupDelaySeconds int
	AdminUsername           string
	AdminPassword           string
	SessionSecret           string
	AllowCustomCommands     bool
	OllamaURL               string
	OllamaModel             string
	LLMAPIKey               string
	AgentSnapshotMinutes    int
	AllowedOrigins          []string
}

func Load() *Config {
	return &Config{
		DatabaseURL:             getEnv("DATABASE_URL", "postgres://amr:amr@localhost:5432/amrdashboard?sslmode=disable"),
		ServerPort:              getEnv("SERVER_PORT", "8080"),
		EncryptionKey:           getEnv("ENCRYPTION_KEY", "change-this-32-byte-secret-key!!"),
		ScheduleAM:              getEnv("SCHEDULE_AM", "0 6 * * *"),
		SchedulePM:              getEnv("SCHEDULE_PM", "0 18 * * *"),
		SyncOnStartup:           getEnvBool("SYNC_ON_STARTUP", false),
		SyncStartupDelaySeconds: getEnvInt("SYNC_STARTUP_DELAY_SECONDS", 20),
		AdminUsername:           getEnv("ADMIN_USERNAME", "admin"),
		AdminPassword:           getEnv("ADMIN_PASSWORD", "admin"),
		SessionSecret:           getEnv("SESSION_SECRET", getEnv("ENCRYPTION_KEY", "change-this-32-byte-secret-key!!")),
		AllowCustomCommands:     getEnvBool("ALLOW_CUSTOM_COMMANDS", false),
		OllamaURL:               getEnv("OLLAMA_URL", "http://localhost:11434"),
		OllamaModel:             getEnv("OLLAMA_MODEL", "llama3"),
		LLMAPIKey:               getEnv("LLM_API_KEY", ""),
		AgentSnapshotMinutes:    getEnvInt("AGENT_SNAPSHOT_MINUTES", 15),
		AllowedOrigins:          getEnvList("ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:5189"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvList(key, fallback string) []string {
	raw := getEnv(key, fallback)
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}
