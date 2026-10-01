package server

import (
	"computer-use-server/internal/cloud"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
)

func TestCapabilityControlsSaveBeforeActivation(t *testing.T) {
	s := fixture(t)
	saved := map[string]bool{}
	s.SaveCapability = func(category string, enabled bool) error {
		_, current := s.Registry.State()
		if current[category] == enabled {
			t.Fatalf("%s activated before saving", category)
		}
		saved[category] = enabled
		return nil
	}
	for category, enabled := range map[string]bool{"files": false, "terminal": true, "computer": true, "browser": false} {
		body, err := json.Marshal(map[string]any{"category": category, "enabled": enabled})
		if err != nil {
			t.Fatal(err)
		}
		if w := request(s.UI(), "POST", "/api/capability", string(body), ""); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if err := s.executeCloudCommand(cloud.Command{Kind: "capability.set", Payload: json.RawMessage(`{"category":"computer","enabled":false}`)}); err != nil {
		t.Fatal(err)
	}
	_, current := s.Registry.State()
	for category, enabled := range saved {
		if current[category] != enabled {
			t.Fatal("saved and active switches differ", saved, current)
		}
	}
	if len(saved) != 4 || saved["computer"] {
		t.Fatal("local and cloud controls did not both save", saved)
	}
}

func TestCapabilitySaveFailureAndInvalidRequestsLeaveStateUnchanged(t *testing.T) {
	s := fixture(t)
	attempts := 0
	s.SaveCapability = func(string, bool) error {
		attempts++
		return errors.New("disk unavailable")
	}
	for _, body := range []string{
		`{"category":"system","enabled":false}`,
		`{"category":"full_access","enabled":true}`,
		`{"category":"unknown","enabled":true}`,
		`{"category":"computer"}`,
		`{"category":"computer","enabled":null}`,
	} {
		if w := request(s.UI(), "POST", "/api/capability", body, ""); w.Code != http.StatusBadRequest {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if attempts != 0 {
		t.Fatal("invalid request saved settings")
	}
	w := request(s.UI(), "POST", "/api/capability", `{"category":"computer","enabled":true}`, "")
	if w.Code != http.StatusInternalServerError {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := s.executeCloudCommand(cloud.Command{Kind: "capability.set", Payload: json.RawMessage(`{"category":"computer","enabled":true}`)}); err == nil {
		t.Fatal("cloud save failure was hidden")
	}
	_, current := s.Registry.State()
	if attempts != 2 || current["computer"] {
		t.Fatal("failed save changed the active switch", attempts, current)
	}
}

func TestConcurrentCapabilityControlsKeepSavedAndActiveStateTogether(t *testing.T) {
	s := fixture(t)
	saved := map[string]bool{}
	s.SaveCapability = func(category string, enabled bool) error {
		saved[category] = enabled
		return nil
	}
	var calls sync.WaitGroup
	for i := 0; i < 40; i++ {
		calls.Add(1)
		go func(i int) {
			defer calls.Done()
			category := []string{"files", "terminal", "computer", "browser"}[i%4]
			if err := s.setCapability(category, i%8 < 4); err != nil {
				t.Error(err)
			}
		}(i)
	}
	calls.Wait()
	_, current := s.Registry.State()
	for category, enabled := range saved {
		if current[category] != enabled {
			t.Fatal("concurrent control lost a setting", saved, current)
		}
	}
}
