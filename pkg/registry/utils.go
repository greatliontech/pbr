package registry

import (
	"crypto/sha256"
	"encoding/hex"
)

func fakeUUID(input string) string {
	// Hash the input string using SHA-256
	hash := sha256.Sum256([]byte(input))

	// Convert the hash to a hexadecimal string
	hexString := hex.EncodeToString(hash[:])

	// Take the first 32 characters to resemble a UUID without dashes
	// Adjust as needed to ensure the hash length matches your UUID requirements
	if len(hexString) > 32 {
		hexString = hexString[:32]
	}

	return hexString
}
