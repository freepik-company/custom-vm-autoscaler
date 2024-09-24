package globals

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

// Get environment variables if defined. If not it retrieves
// a default value
func GetEnv(key string, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultVal
}

// IsInCriticalPeriod checks if the current time is within the critical period
func IsInCriticalPeriod(currentTime time.Time, criticalPeriodHours []string, criticalPeriodDays []string) bool {
	currentWeekday := int(currentTime.Weekday())
	for _, criticalPeriodDay := range criticalPeriodDays {
		if strings.TrimSpace(criticalPeriodDay) == strconv.Itoa(currentWeekday) {
			startHour, err := time.Parse("15:04:05", criticalPeriodHours[0])
			if err != nil {
				log.Printf("Error parsing start hour: %v", err)
				return false
			}
			endHour, err := time.Parse("15:04:05", criticalPeriodHours[1])
			if err != nil {
				log.Printf("Error parsing end hour: %v", err)
				return false
			}

			// Adjust start and end times to match the current date
			startTime := time.Date(currentTime.Year(), currentTime.Month(), currentTime.Day(), startHour.Hour(), startHour.Minute(), startHour.Second(), 0, currentTime.Location())
			endTime := time.Date(currentTime.Year(), currentTime.Month(), currentTime.Day(), endHour.Hour(), endHour.Minute(), endHour.Second(), 0, currentTime.Location())

			if currentTime.After(startTime) && currentTime.Before(endTime) {
				return true
			}
		}
	}
	return false
}
