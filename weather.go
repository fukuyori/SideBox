package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const forecastEndpoint = "https://weather.tsukumijima.net/api/forecast/city/"

type weatherClient struct {
	httpClient *http.Client
}

type weatherReport struct {
	Location    string
	Description string
	Detail      string
	PublishedAt time.Time
	Daily       []dailyForecast
}

type dailyForecast struct {
	Date                     time.Time
	DateLabel                string
	Description              string
	Wind                     string
	TemperatureMax           *float64
	TemperatureMin           *float64
	PrecipitationProbability *int
}

func newWeatherClient() *weatherClient {
	return &weatherClient{httpClient: &http.Client{Timeout: 12 * time.Second}}
}

func (c *weatherClient) fetch(ctx context.Context, cfg appConfig) (weatherReport, error) {
	if _, err := strconv.Atoi(cfg.CityCode); err != nil {
		return weatherReport{}, fmt.Errorf("地域コードが不正です: %s", cfg.CityCode)
	}

	var response struct {
		PublicTime  string `json:"publicTime"`
		Title       string `json:"title"`
		Description struct {
			Text string `json:"text"`
		} `json:"description"`
		Location struct {
			Prefecture string `json:"prefecture"`
			District   string `json:"district"`
			City       string `json:"city"`
		} `json:"location"`
		Forecasts []struct {
			Date      string `json:"date"`
			DateLabel string `json:"dateLabel"`
			Telop     string `json:"telop"`
			Detail    struct {
				Weather string `json:"weather"`
				Wind    string `json:"wind"`
			} `json:"detail"`
			Temperature struct {
				Min struct {
					Celsius *string `json:"celsius"`
				} `json:"min"`
				Max struct {
					Celsius *string `json:"celsius"`
				} `json:"max"`
			} `json:"temperature"`
			ChanceOfRain map[string]string `json:"chanceOfRain"`
		} `json:"forecasts"`
	}
	endpoint := forecastEndpoint + url.PathEscape(cfg.CityCode)
	if err := c.getJSON(ctx, endpoint, &response); err != nil {
		return weatherReport{}, fmt.Errorf("天気情報を取得できません: %w", err)
	}
	if len(response.Forecasts) == 0 {
		return weatherReport{}, fmt.Errorf("地域コード %s の予報がありません", cfg.CityCode)
	}

	publishedAt, _ := time.Parse(time.RFC3339, response.PublicTime)
	report := weatherReport{
		Location:    strings.TrimSpace(response.Location.Prefecture + " " + response.Location.City),
		Description: response.Forecasts[0].Telop,
		Detail:      compactJapaneseText(response.Description.Text),
		PublishedAt: publishedAt,
	}
	for _, forecast := range response.Forecasts {
		date, err := time.Parse("2006-01-02", forecast.Date)
		if err != nil {
			continue
		}
		report.Daily = append(report.Daily, dailyForecast{
			Date:                     date,
			DateLabel:                forecast.DateLabel,
			Description:              forecast.Telop,
			Wind:                     normalizeSpaces(forecast.Detail.Wind),
			TemperatureMax:           parseOptionalFloat(forecast.Temperature.Max.Celsius),
			TemperatureMin:           parseOptionalFloat(forecast.Temperature.Min.Celsius),
			PrecipitationProbability: maximumRainChance(forecast.ChanceOfRain),
		})
	}
	return report, nil
}

func (c *weatherClient) getJSON(ctx context.Context, endpoint string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "sidebox/"+appVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(target); err != nil {
		return fmt.Errorf("応答を読み取れません: %w", err)
	}
	return nil
}

func parseOptionalFloat(value *string) *float64 {
	if value == nil || *value == "" {
		return nil
	}
	parsed, err := strconv.ParseFloat(*value, 64)
	if err != nil {
		return nil
	}
	return &parsed
}

func maximumRainChance(values map[string]string) *int {
	var maximum int
	found := false
	for _, value := range values {
		value = strings.TrimSuffix(strings.TrimSpace(value), "%")
		parsed, err := strconv.Atoi(value)
		if err != nil {
			continue
		}
		if !found || parsed > maximum {
			maximum = parsed
			found = true
		}
	}
	if !found {
		return nil
	}
	return &maximum
}

func normalizeSpaces(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func compactJapaneseText(value string) string {
	return strings.ReplaceAll(normalizeSpaces(value), "　", "")
}
