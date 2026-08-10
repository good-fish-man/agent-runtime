package dispatcher

import "testing"

func TestDecodeJSONObjectAcceptsFencedModelOutput(t *testing.T) {
	var result struct {
		Queries []struct {
			Text string `json:"text"`
		} `json:"queries"`
	}
	if err := decodeJSONObject("Here is the result:\n```json\n{\"queries\":[{\"text\":\"official docs\"}]}\n```", &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Queries) != 1 || result.Queries[0].Text != "official docs" {
		t.Fatalf("unexpected decoded output: %+v", result)
	}
}

func TestDecodeJSONObjectRejectsMissingObject(t *testing.T) {
	var result map[string]any
	if err := decodeJSONObject("no structured output", &result); err == nil {
		t.Fatal("expected malformed advisor output to be rejected")
	}
}
