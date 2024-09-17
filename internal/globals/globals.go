package globals

import "os"

// Get environment variables if defined. If not it retrieves
// a default value
func GetEnv(key string, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultVal
}
