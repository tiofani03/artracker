package handler

import (
	"github.com/tiofani03/artracker/backend/internal/people"
)

// PeopleStore returns the end-user profile store; nil-safe (its methods no-op) when the database is disabled.
func (h *Handler) PeopleStore() *people.Store {
	if h.db == nil {
		return nil
	}
	return people.NewStore(h.db)
}
