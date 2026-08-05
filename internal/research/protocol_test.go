package research

import "testing"

func TestSourceTrustPrioritizesAuthoritativeDomains(t *testing.T) {
	government, governmentLevel := sourceTrust("https://data.gov/report", "Official report")
	unknown, unknownLevel := sourceTrust("http://random.example/post", "A post")
	if government <= unknown || governmentLevel != "high" || unknownLevel != "low" {
		t.Fatalf("unexpected trust scores government=%f/%s unknown=%f/%s", government, governmentLevel, unknown, unknownLevel)
	}
}
