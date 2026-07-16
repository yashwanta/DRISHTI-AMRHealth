package config

import (
	"os"
	"strings"
)

// PlantConfig describes a single site with a RoboWatch/RDS/FleetManager instance.
type PlantConfig struct {
	Name       string // Display name, e.g. "Springfield"
	SystemType string // e.g. "RoboWatch", "FleetManager"
	BaseURL    string // e.g. "http://10.222.10.76:8080/"
	Port       int    // e.g. 8080
	Username   string // read-only account on the RoboWatch system
}

// AllPlants returns the list of known plants.
// Passwords are NOT stored here - they come from environment variables at runtime.
// To add a new plant: add an entry here AND set its ROBOWATCH_<PLANT>_PASSWORD env var.
func AllPlants() []PlantConfig {
	return []PlantConfig{
		{
			Name:       "Springfield",
			SystemType: "RoboWatch / FleetManager",
			BaseURL:    "http://10.222.10.76:8080/",
			Port:       8080,
			Username:   "robowatch",
		},
		{
			Name:       "Hopkinsville",
			SystemType: "RoboWatch / FleetManager",
			BaseURL:    "http://10.216.4.59:8080/",
			Port:       8080,
			Username:   "robowatch",
		},
		{
			Name:       "Shelbyville",
			SystemType: "RoboWatch / FleetManager",
			BaseURL:    "http://10.205.22.12:8080/",
			Port:       8080,
			Username:   "robowatch",
		},
	}
}

// GetPlant returns the PlantConfig for a given plant name, or nil if not found.
func GetPlant(name string) *PlantConfig {
	for _, p := range AllPlants() {
		if p.Name == name {
			return &p
		}
	}
	return nil
}

// GetRobowatchPassword returns the password for a given plant from the ROBOWATCH_<PLANT>_PASSWORD env var.
// The plant name is uppercased and sanitized to form the env var name.
func GetRobowatchPassword(plant string) string {
	key := "ROBOWATCH_" + strings.ToUpper(strings.ReplaceAll(plant, " ", "_")) + "_PASSWORD"
	return os.Getenv(key)
}

// PlantForServer infers a plant name from a server's display name and host address.
// It matches known plant keywords in the server name first; if none match it falls
// back to IP prefix (10.222.* → Springfield, 10.216.* → Hopkinsville, 10.205.* → Shelbyville).
// Returns "" if the plant cannot be determined.
func PlantForServer(serverName, serverHost string) string {
	low := strings.ToLower(serverName)
	switch {
	case strings.Contains(low, "springfield"):
		return "Springfield"
	case strings.Contains(low, "hop"), strings.Contains(low, "hopkinsville"):
		return "Hopkinsville"
	case strings.Contains(low, "shelbyville"), strings.Contains(low, "shelby"):
		return "Shelbyville"
	}
	// Fallback: IP prefix
	switch {
	case strings.HasPrefix(serverHost, "10.222."):
		return "Springfield"
	case strings.HasPrefix(serverHost, "10.216."):
		return "Hopkinsville"
	case strings.HasPrefix(serverHost, "10.205."):
		return "Shelbyville"
	}
	return ""
}
