package main

import "testing"

func TestMaximumRainChance(t *testing.T) {
	got := maximumRainChance(map[string]string{
		"T00_06": "--%",
		"T06_12": "10%",
		"T12_18": "30%",
		"T18_24": "20%",
	})
	if got == nil || *got != 30 {
		t.Fatalf("maximumRainChance() = %v, want 30", got)
	}
}

func TestMaximumRainChanceMissing(t *testing.T) {
	if got := maximumRainChance(map[string]string{"T00_06": "--%"}); got != nil {
		t.Fatalf("maximumRainChance() = %v, want nil", *got)
	}
}

func TestParseOptionalFloat(t *testing.T) {
	value := "35"
	got := parseOptionalFloat(&value)
	if got == nil || *got != 35 {
		t.Fatalf("parseOptionalFloat() = %v, want 35", got)
	}
	if got := parseOptionalFloat(nil); got != nil {
		t.Fatalf("parseOptionalFloat(nil) = %v, want nil", *got)
	}
}
