package repository

import "time"

// nowUTC stamps a newly opened position. The repository owns persistence
// timestamps; business time still comes from the injected clock in the service.
func nowUTC() time.Time { return time.Now().UTC() }
