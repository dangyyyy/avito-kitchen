package domain

import (
	"time"

	"github.com/google/uuid"
)

// User exists so orders have an owner. Registration and authentication are out
// of scope, so users arrive through seed data.
type User struct {
	ID        uuid.UUID
	Name      string
	Phone     string
	CreatedAt time.Time
}
