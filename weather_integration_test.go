package main

import (
	"context"
	"os"
	"testing"
)

func TestWeatherClientLive(t *testing.T) {
	if os.Getenv("SIDEBOX_LIVE_TEST") != "1" {
		t.Skip("set SIDEBOX_LIVE_TEST=1 to call the live weather API")
	}
	report, err := newWeatherClient().fetch(context.Background(), appConfig{CityCode: "130010"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Location == "" || len(report.Daily) == 0 {
		t.Fatalf("incomplete weather report: %+v", report)
	}
	if report.Daily[0].TemperatureMin == nil || report.Daily[0].TemperatureMax == nil {
		t.Fatalf("today's temperatures were not completed: %+v", report.Daily[0])
	}
	t.Logf("today: min %.1f, max %.1f", *report.Daily[0].TemperatureMin, *report.Daily[0].TemperatureMax)
}
