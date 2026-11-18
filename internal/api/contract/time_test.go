package contract

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNullableTimeSendsNeverAsNull(t *testing.T) {
	body, err := json.Marshal(struct {
		Never *time.Time `json:"never"`
		At    *time.Time `json:"at"`
	}{NullableTime(time.Time{}), NullableTime(time.Date(2026, 11, 18, 9, 30, 0, 0, time.UTC))})
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"never":null,"at":"2026-11-18T09:30:00Z"}` {
		t.Fatalf("body = %s", body)
	}
}
