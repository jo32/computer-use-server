package server

import (
	"errors"
	"fmt"
)

func capabilityCategory(category string) bool {
	return category == "files" || category == "terminal" || category == "computer" || category == "browser"
}

// Serialize persistence and activation across local and bound-account controls.
func (s *Server) setCapability(category string, enabled bool) error {
	if !capabilityCategory(category) {
		return errors.New("unknown capability")
	}
	s.capabilityMu.Lock()
	defer s.capabilityMu.Unlock()
	// A failed save must leave the current setting unchanged.
	if s.SaveCapability != nil {
		if err := s.SaveCapability(category, enabled); err != nil {
			return fmt.Errorf("save capability: %w", err)
		}
	}
	if err := s.Registry.Enable(category, enabled); err != nil {
		return err
	}
	if s.Chrome != nil {
		s.Chrome.Refresh()
	}
	return nil
}
