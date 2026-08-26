package main

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestWeatherClientLive(t *testing.T) {
	if os.Getenv("SIDEBOX_LIVE_TEST") != "1" {
		t.Skip("set SIDEBOX_LIVE_TEST=1 to call the live weather API")
	}
	locations := map[string]string{"130010": "東京都 東京地方", "140010": "神奈川県 東部"}
	for _, cityCode := range []string{"130010", "140010"} {
		t.Run(cityCode, func(t *testing.T) {
			report, err := newWeatherClient().fetch(context.Background(), appConfig{CityCode: cityCode})
			if err != nil {
				t.Fatal(err)
			}
			if report.Location == "" || len(report.Daily) == 0 {
				t.Fatalf("incomplete weather report: %+v", report)
			}
			if report.Location != locations[cityCode] {
				t.Fatalf("Location = %q, want %q", report.Location, locations[cityCode])
			}
			if report.Daily[0].TemperatureMin == nil || report.Daily[0].TemperatureMax == nil {
				t.Fatalf("today's temperatures were not completed: %+v", report.Daily[0])
			}
			if report.Humidity == nil {
				t.Fatalf("today's humidity was not completed: %+v", report)
			}
			today := time.Now().In(report.Daily[0].Date.Location()).Format("2006-01-02")
			if got := report.Daily[0].Date.Format("2006-01-02"); got != today {
				t.Fatalf("today date = %s, want %s", got, today)
			}
			t.Logf("%s: min %.1f, max %.1f, humidity %d", report.Location, *report.Daily[0].TemperatureMin, *report.Daily[0].TemperatureMax, *report.Humidity)
			if len(report.Daily) >= 3 {
				if report.Daily[2].TemperatureMin == nil || report.Daily[2].TemperatureMax == nil || report.Daily[2].PrecipitationProbability == nil {
					t.Fatalf("day-after forecast was incomplete: %+v", report.Daily[2])
				}
				t.Logf("day after: min %.1f, max %.1f, rain %d", *report.Daily[2].TemperatureMin, *report.Daily[2].TemperatureMax, *report.Daily[2].PrecipitationProbability)
			}
		})
	}
}
