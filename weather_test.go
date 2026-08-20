package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

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

func TestWeatherClientFillsOnlyMissingTodayTemperatureFromAmedas(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/forecast/130000.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{
			"publishingOffice":"気象庁",
			"reportDatetime":"2026-08-04T11:00:00+09:00",
			"timeSeries":[
				{
					"timeDefines":["2026-08-04T11:00:00+09:00","2026-08-05T00:00:00+09:00","2026-08-06T00:00:00+09:00"],
					"areas":[{"area":{"name":"東京地方","code":"130010"},"weathers":["くもり","晴れ","雨"],"winds":["南の風","北の風","東の風"]}]
				},
				{
					"timeDefines":["2026-08-04T12:00:00+09:00","2026-08-04T18:00:00+09:00","2026-08-05T00:00:00+09:00"],
					"areas":[{"area":{"name":"東京地方","code":"130010"},"pops":["20","40","10"]}]
				},
				{
					"timeDefines":["2026-08-04T09:00:00+09:00","2026-08-04T00:00:00+09:00","2026-08-05T00:00:00+09:00","2026-08-05T09:00:00+09:00"],
					"areas":[{"area":{"name":"東京","code":"44132"},"temps":["28","28","23","30"]}]
				}
			]
		},{
			"publishingOffice":"気象庁",
			"reportDatetime":"2026-08-04T11:00:00+09:00",
			"timeSeries":[
				{
					"timeDefines":["2026-08-05T00:00:00+09:00","2026-08-06T00:00:00+09:00"],
					"areas":[{"area":{"name":"東京都","code":"130000"},"weatherCodes":["100","300"],"pops":["","60"]}]
				},
				{
					"timeDefines":["2026-08-05T00:00:00+09:00","2026-08-06T00:00:00+09:00"],
					"areas":[{"area":{"name":"東京","code":"44132"},"tempsMin":["","21"],"tempsMax":["","29"]}]
				}
			]
		}]`)
	})
	mux.HandleFunc("/amedas-min.csv", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		_, _ = io.WriteString(w, "44132,prefecture,station,47662,2026,08,04,07,00,22.0,4,05,28,4\n")
	})
	mux.HandleFunc("/amedas-max.csv", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "maximum endpoint must not be called", http.StatusInternalServerError)
	})
	mux.HandleFunc("/amedas/latest_time.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "2026-08-04T12:10:00+09:00\n")
	})
	mux.HandleFunc("/amedas/map/20260804121000.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"44132":{"humidity":[63,0]}}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := &weatherClient{
		httpClient:         server.Client(),
		forecastEndpoint:   server.URL + "/forecast/",
		jmaMinimumEndpoint: server.URL + "/amedas-min.csv",
		jmaMaximumEndpoint: server.URL + "/amedas-max.csv",
		jmaLatestEndpoint:  server.URL + "/amedas/latest_time.txt",
		jmaAmedasEndpoint:  server.URL + "/amedas/map/",
	}
	report, err := client.fetch(context.Background(), appConfig{CityCode: "130010"})
	if err != nil {
		t.Fatal(err)
	}
	if got := report.Daily[0].TemperatureMin; got == nil || *got != 22.0 {
		t.Fatalf("TemperatureMin = %v, want observed 22.0", got)
	}
	if got := report.Daily[0].TemperatureMax; got == nil || *got != 28.0 {
		t.Fatalf("TemperatureMax = %v, want forecast 28.0", got)
	}
	if got := report.Daily[0].PrecipitationProbability; got == nil || *got != 40 {
		t.Fatalf("PrecipitationProbability = %v, want 40", got)
	}
	if report.Location != "東京都 東京地方" {
		t.Fatalf("Location = %q, want 東京都 東京地方", report.Location)
	}
	if report.Humidity == nil || *report.Humidity != 63 {
		t.Fatalf("Humidity = %v, want 63", report.Humidity)
	}
	if got := report.Daily[2].TemperatureMin; got == nil || *got != 21 {
		t.Fatalf("day-after TemperatureMin = %v, want 21", got)
	}
	if got := report.Daily[2].TemperatureMax; got == nil || *got != 29 {
		t.Fatalf("day-after TemperatureMax = %v, want 29", got)
	}
	if got := report.Daily[2].PrecipitationProbability; got == nil || *got != 60 {
		t.Fatalf("day-after PrecipitationProbability = %v, want 60", got)
	}
}

func TestJMALocationName(t *testing.T) {
	tests := []struct {
		cityCode string
		areaName string
		want     string
	}{
		{"130010", "東京地方", "東京都 東京地方"},
		{"140010", "東部", "神奈川県 東部"},
		{"016010", "石狩地方", "北海道 石狩地方"},
		{"invalid", "東部", "東部"},
	}
	for _, tt := range tests {
		if got := jmaLocationName(tt.cityCode, tt.areaName); got != tt.want {
			t.Errorf("jmaLocationName(%q, %q) = %q, want %q", tt.cityCode, tt.areaName, got, tt.want)
		}
	}
}
