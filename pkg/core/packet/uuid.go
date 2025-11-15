package packet

import (
	"crypto/rand"
	"fmt"
	"time"
)

// generateUUID создает простой UUID v4
func generateUUID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		// Fallback на timestamp
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	
	// UUID v4
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	
	return fmt.Sprintf("%x-%x-%x-%x-%x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
