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
	mux.HandleFunc("/forecast/130010", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"publicTime":"2026-08-04T05:00:00+09:00",
			"link":"https://www.jma.go.jp/bosai/forecast/#area_type=offices&area_code=130000",
			"location":{"prefecture":"東京都","city":"東京"},
			"forecasts":[{
				"date":"2026-08-04","dateLabel":"今日","telop":"曇り",
				"detail":{"wind":"南の風"},
				"temperature":{"min":{"celsius":null},"max":{"celsius":"28"}},
				"chanceOfRain":{"T06_12":"20%"}
			}]
		}`)
	})
	mux.HandleFunc("/jma-forecast/130000.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"timeSeries":[{},{},{"areas":[{"area":{"name":"東京","code":"44132"}}]}]}]`)
	})
	mux.HandleFunc("/amedas-min.csv", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		_, _ = io.WriteString(w, "44132,prefecture,station,47662,2026,08,04,07,00,22.0,4,05,28,4\n")
	})
	mux.HandleFunc("/amedas-max.csv", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "maximum endpoint must not be called", http.StatusInternalServerError)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := &weatherClient{
		httpClient:          server.Client(),
		forecastEndpoint:    server.URL + "/forecast/",
		jmaForecastEndpoint: server.URL + "/jma-forecast/",
		jmaMinimumEndpoint:  server.URL + "/amedas-min.csv",
		jmaMaximumEndpoint:  server.URL + "/amedas-max.csv",
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
}
