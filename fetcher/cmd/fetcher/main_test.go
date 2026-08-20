package main

import "testing"

func TestIDSetAcceptsRepeatedCommaSeparatedValues(t *testing.T) {
	var values idSet
	if err := values.Set("27, 17"); err != nil {
		t.Fatal(err)
	}
	if err := values.Set("27"); err != nil {
		t.Fatal(err)
	}
	if len(values.values) != 2 {
		t.Fatalf("values = %#v", values.values)
	}
	if _, ok := values.values[27]; !ok {
		t.Fatal("workspace 27 missing")
	}
	if _, ok := values.values[17]; !ok {
		t.Fatal("workspace 17 missing")
	}
}

func TestIDSetRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"", "27,", "zero", "0", "-1"} {
		var values idSet
		if err := values.Set(value); err == nil {
			t.Errorf("%q was accepted", value)
		}
	}
}
