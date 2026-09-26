package platform

import "testing"

func TestLoadConfigDefaultsWhenMissing(t *testing.T) {
	c, err := LoadConfig(t.TempDir() + "/nope.json")
	if err != nil {
		t.Fatal(err)
	}
	if c.API.Listen != ":8080" || c.Relay.TTLDays != 7 {
		t.Fatalf("bad defaults: %+v", c)
	}
}
