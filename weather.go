package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultForecastEndpoint    = "https://weather.tsukumijima.net/api/forecast/city/"
	defaultJMAForecastEndpoint = "https://www.jma.go.jp/bosai/forecast/data/forecast/"
	defaultJMAMinimumEndpoint  = "https://www.data.jma.go.jp/stats/data/mdrr/tem_rct/alltable/mntemsadext00_rct.csv"
	defaultJMAMaximumEndpoint  = "https://www.data.jma.go.jp/stats/data/mdrr/tem_rct/alltable/mxtemsadext00_rct.csv"
)

type weatherClient struct {
	httpClient          *http.Client
	forecastEndpoint    string
	jmaForecastEndpoint string
	jmaMinimumEndpoint  string
	jmaMaximumEndpoint  string
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
	return &weatherClient{
		httpClient:          &http.Client{Timeout: 12 * time.Second},
		forecastEndpoint:    defaultForecastEndpoint,
		jmaForecastEndpoint: defaultJMAForecastEndpoint,
		jmaMinimumEndpoint:  defaultJMAMinimumEndpoint,
		jmaMaximumEndpoint:  defaultJMAMaximumEndpoint,
	}
}

func (c *weatherClient) fetch(ctx context.Context, cfg appConfig) (weatherReport, error) {
	if _, err := strconv.Atoi(cfg.CityCode); err != nil {
		return weatherReport{}, fmt.Errorf("地域コードが不正です: %s", cfg.CityCode)
	}

	var response struct {
		PublicTime  string `json:"publicTime"`
		Title       string `json:"title"`
		Link        string `json:"link"`
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
	endpoint := c.forecastEndpoint + url.PathEscape(cfg.CityCode)
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
	if len(report.Daily) > 0 && (report.Daily[0].TemperatureMin == nil || report.Daily[0].TemperatureMax == nil) {
		// The forecast API omits today's temperature extrema after their forecast
		// period. Keep forecast values where present and fill only missing values
		// from observations at the same JMA temperature station.
		_ = c.fillTodayTemperatureFromAmedas(ctx, response.Link, cfg.CityCode, response.Location.City, &report.Daily[0])
	}
	return report, nil
}

func (c *weatherClient) fillTodayTemperatureFromAmedas(ctx context.Context, forecastLink, cityCode, stationName string, today *dailyForecast) error {
	officeCode, err := jmaOfficeCode(forecastLink, cityCode)
	if err != nil {
		return err
	}

	var forecastData []struct {
		TimeSeries []struct {
			Areas []struct {
				Area struct {
					Name string `json:"name"`
					Code string `json:"code"`
				} `json:"area"`
			} `json:"areas"`
		} `json:"timeSeries"`
	}
	if err := c.getJSON(ctx, c.jmaForecastEndpoint+url.PathEscape(officeCode)+".json", &forecastData); err != nil {
		return fmt.Errorf("気象庁の予報地点を取得できません: %w", err)
	}

	stationCode := findTemperatureStationCode(forecastData, stationName)
	if stationCode == "" {
		return fmt.Errorf("気象庁の予報地点 %s に対応する観測所がありません", stationName)
	}

	var firstError error
	if today.TemperatureMin == nil {
		observedMin, err := c.getAmedasDailyExtreme(ctx, c.jmaMinimumEndpoint, stationCode, today.Date)
		if err != nil {
			firstError = err
		} else {
			today.TemperatureMin = observedMin
		}
	}
	if today.TemperatureMax == nil {
		observedMax, err := c.getAmedasDailyExtreme(ctx, c.jmaMaximumEndpoint, stationCode, today.Date)
		if err != nil && firstError == nil {
			firstError = err
		} else if err == nil {
			today.TemperatureMax = observedMax
		}
	}
	return firstError
}

func jmaOfficeCode(forecastLink, cityCode string) (string, error) {
	if parsed, err := url.Parse(forecastLink); err == nil {
		if values, err := url.ParseQuery(parsed.Fragment); err == nil {
			if code := values.Get("area_code"); isNumericCode(code, 6) {
				return code, nil
			}
		}
	}
	if isNumericCode(cityCode, 6) {
		return cityCode[:3] + "000", nil
	}
	return "", fmt.Errorf("気象庁の予報区コードを特定できません")
}

func isNumericCode(value string, length int) bool {
	if len(value) != length {
		return false
	}
	_, err := strconv.Atoi(value)
	return err == nil
}

func findTemperatureStationCode(forecastData []struct {
	TimeSeries []struct {
		Areas []struct {
			Area struct {
				Name string `json:"name"`
				Code string `json:"code"`
			} `json:"area"`
		} `json:"areas"`
	} `json:"timeSeries"`
}, stationName string) string {
	if len(forecastData) == 0 {
		return ""
	}
	for _, series := range forecastData[0].TimeSeries {
		for _, area := range series.Areas {
			if area.Area.Name == stationName && isNumericCode(area.Area.Code, 5) {
				return area.Area.Code
			}
		}
	}
	return ""
}

func (c *weatherClient) getAmedasDailyExtreme(ctx context.Context, endpoint, stationCode string, date time.Time) (*float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "sidebox/"+appVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	// The JMA CSV is Shift_JIS, but the station code, date and temperature
	// columns used here are ASCII and can be parsed without converting names.
	reader := csv.NewReader(io.LimitReader(resp.Body, 8<<20))
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("アメダス実測値を読み取れません: %w", err)
		}
		if len(record) < 10 || record[0] != stationCode {
			continue
		}
		if record[4] != date.Format("2006") || record[5] != date.Format("01") || record[6] != date.Format("02") {
			continue
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(record[9]), 64)
		if err != nil {
			return nil, fmt.Errorf("アメダス実測値が不正です: %w", err)
		}
		return &value, nil
	}
	return nil, fmt.Errorf("観測所 %s の当日実測値がありません", stationCode)
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
