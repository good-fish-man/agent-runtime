package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCapabilitiesHandlerListsCatalog(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/capabilities", nil)
	makeCapabilitiesHandler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Capabilities []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Capabilities) == 0 {
		t.Fatal("capability catalog is empty")
	}
}
