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
	defaultForecastEndpoint   = "https://www.jma.go.jp/bosai/forecast/data/forecast/"
	defaultJMAMinimumEndpoint = "https://www.data.jma.go.jp/stats/data/mdrr/tem_rct/alltable/mntemsadext00_rct.csv"
	defaultJMAMaximumEndpoint = "https://www.data.jma.go.jp/stats/data/mdrr/tem_rct/alltable/mxtemsadext00_rct.csv"
	defaultJMALatestEndpoint  = "https://www.jma.go.jp/bosai/amedas/data/latest_time.txt"
	defaultJMAAmedasEndpoint  = "https://www.jma.go.jp/bosai/amedas/data/map/"
)

type weatherClient struct {
	httpClient         *http.Client
	forecastEndpoint   string
	jmaMinimumEndpoint string
	jmaMaximumEndpoint string
	jmaLatestEndpoint  string
	jmaAmedasEndpoint  string
}

type jmaForecastArea struct {
	Area struct {
		Name string `json:"name"`
		Code string `json:"code"`
	} `json:"area"`
	WeatherCodes []string `json:"weatherCodes"`
	Weathers     []string `json:"weathers"`
	Winds        []string `json:"winds"`
	Pops         []string `json:"pops"`
	Temps        []string `json:"temps"`
	TempsMin     []string `json:"tempsMin"`
	TempsMax     []string `json:"tempsMax"`
}

type jmaForecastTimeSeries struct {
	TimeDefines []string          `json:"timeDefines"`
	Areas       []jmaForecastArea `json:"areas"`
}

type jmaForecastBlock struct {
	PublishingOffice string                  `json:"publishingOffice"`
	ReportDatetime   string                  `json:"reportDatetime"`
	TimeSeries       []jmaForecastTimeSeries `json:"timeSeries"`
}

type weatherReport struct {
	Location    string
	Description string
	Detail      string
	PublishedAt time.Time
	Humidity    *int
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
		httpClient:         &http.Client{Timeout: 12 * time.Second},
		forecastEndpoint:   defaultForecastEndpoint,
		jmaMinimumEndpoint: defaultJMAMinimumEndpoint,
		jmaMaximumEndpoint: defaultJMAMaximumEndpoint,
		jmaLatestEndpoint:  defaultJMALatestEndpoint,
		jmaAmedasEndpoint:  defaultJMAAmedasEndpoint,
	}
}

func (c *weatherClient) fetch(ctx context.Context, cfg appConfig) (weatherReport, error) {
	if !isNumericCode(cfg.CityCode, 6) {
		return weatherReport{}, fmt.Errorf("地域コードが不正です: %s", cfg.CityCode)
	}
	officeCode, err := jmaOfficeCode(cfg.CityCode)
	if err != nil {
		return weatherReport{}, err
	}
	var response []jmaForecastBlock
	endpoint := c.forecastEndpoint + url.PathEscape(officeCode) + ".json"
	if err := c.getJSON(ctx, endpoint, &response); err != nil {
		return weatherReport{}, fmt.Errorf("気象庁から天気情報を取得できません: %w", err)
	}
	if len(response) == 0 {
		return weatherReport{}, fmt.Errorf("地域コード %s の予報がありません", cfg.CityCode)
	}
	report, err := c.buildJMAReport(ctx, response[0], cfg.CityCode)
	if err != nil {
		return weatherReport{}, err
	}
	if len(response) > 1 {
		mergeJMAWeeklyForecast(&report, response[1], cfg.CityCode)
	}
	return report, nil
}

func (c *weatherClient) buildJMAReport(ctx context.Context, block jmaForecastBlock, cityCode string) (weatherReport, error) {
	weatherSeries, weatherArea, areaIndex, ok := findJMASeriesArea(block.TimeSeries, cityCode, func(area jmaForecastArea) bool {
		return len(area.Weathers) > 0
	})
	if !ok {
		return weatherReport{}, fmt.Errorf("地域コード %s の短期予報がありません", cityCode)
	}
	publishedAt, _ := time.Parse(time.RFC3339, block.ReportDatetime)
	report := weatherReport{Location: jmaLocationName(cityCode, weatherArea.Area.Name), PublishedAt: publishedAt}
	dailyIndexes := make(map[string]int)
	for index, value := range weatherArea.Weathers {
		if index >= len(weatherSeries.TimeDefines) {
			break
		}
		dateTime, err := time.Parse(time.RFC3339, weatherSeries.TimeDefines[index])
		if err != nil {
			continue
		}
		forecast := dailyForecast{
			Date:        dateTime,
			DateLabel:   jmaDateLabel(len(report.Daily)),
			Description: compactJapaneseText(value),
		}
		if index < len(weatherArea.Winds) {
			forecast.Wind = normalizeSpaces(weatherArea.Winds[index])
		}
		dailyIndexes[dateTime.Format("2006-01-02")] = len(report.Daily)
		report.Daily = append(report.Daily, forecast)
	}
	if len(report.Daily) == 0 {
		return weatherReport{}, fmt.Errorf("地域コード %s の予報日がありません", cityCode)
	}
	report.Description = report.Daily[0].Description

	if rainSeries, rainArea, _, found := findJMASeriesArea(block.TimeSeries, cityCode, func(area jmaForecastArea) bool {
		return len(area.Pops) > 0
	}); found {
		rainByDate := make(map[string]map[string]string)
		for index, value := range rainArea.Pops {
			if index >= len(rainSeries.TimeDefines) || value == "" {
				continue
			}
			dateTime, err := time.Parse(time.RFC3339, rainSeries.TimeDefines[index])
			if err != nil {
				continue
			}
			dateKey := dateTime.Format("2006-01-02")
			if rainByDate[dateKey] == nil {
				rainByDate[dateKey] = make(map[string]string)
			}
			rainByDate[dateKey][rainSeries.TimeDefines[index]] = value
		}
		for dateKey, values := range rainByDate {
			if index, exists := dailyIndexes[dateKey]; exists {
				report.Daily[index].PrecipitationProbability = maximumRainChance(values)
			}
		}
	}

	stationCode := ""
	for _, series := range block.TimeSeries {
		if len(series.Areas) == 0 || len(series.Areas[0].Temps) == 0 {
			continue
		}
		temperatureIndex := areaIndex
		if temperatureIndex >= len(series.Areas) {
			temperatureIndex = 0
		}
		area := series.Areas[temperatureIndex]
		stationCode = area.Area.Code
		var previousTime time.Time
		for index, value := range area.Temps {
			if index >= len(series.TimeDefines) {
				break
			}
			dateTime, err := time.Parse(time.RFC3339, series.TimeDefines[index])
			if err != nil {
				continue
			}
			// After a forecast period has passed, JMA may retain its time slot
			// after a later slot and repeat another temperature value. Treat an
			// out-of-order slot as unavailable so AMeDAS can fill today's gap.
			if !previousTime.IsZero() && dateTime.Before(previousTime) {
				continue
			}
			previousTime = dateTime
			dailyIndex, exists := dailyIndexes[dateTime.Format("2006-01-02")]
			if !exists {
				continue
			}
			temperature := parseOptionalFloat(&value)
			switch dateTime.Hour() {
			case 0:
				report.Daily[dailyIndex].TemperatureMin = temperature
			case 9:
				report.Daily[dailyIndex].TemperatureMax = temperature
			}
		}
		break
	}
	if isNumericCode(stationCode, 5) && (report.Daily[0].TemperatureMin == nil || report.Daily[0].TemperatureMax == nil) {
		_ = c.fillTodayTemperatureFromAmedas(ctx, stationCode, &report.Daily[0])
	}
	if isNumericCode(stationCode, 5) {
		if humidity, err := c.getLatestHumidity(ctx, stationCode); err == nil {
			report.Humidity = humidity
		}
	}
	return report, nil
}

func findJMASeriesArea(series []jmaForecastTimeSeries, cityCode string, hasValues func(jmaForecastArea) bool) (jmaForecastTimeSeries, jmaForecastArea, int, bool) {
	for _, item := range series {
		for index, area := range item.Areas {
			if area.Area.Code == cityCode && hasValues(area) {
				return item, area, index, true
			}
		}
	}
	return jmaForecastTimeSeries{}, jmaForecastArea{}, 0, false
}

func mergeJMAWeeklyForecast(report *weatherReport, block jmaForecastBlock, cityCode string) {
	if report == nil || len(report.Daily) == 0 {
		return
	}
	dailyIndexes := make(map[string]int, len(report.Daily))
	for index, forecast := range report.Daily {
		dailyIndexes[forecast.Date.Format("2006-01-02")] = index
	}

	weeklySeries, weeklyArea, areaIndex, found := findJMASeriesArea(block.TimeSeries, cityCode, func(area jmaForecastArea) bool {
		return len(area.Pops) > 0 || len(area.WeatherCodes) > 0
	})
	if !found {
		if officeCode, err := jmaOfficeCode(cityCode); err == nil {
			weeklySeries, weeklyArea, areaIndex, found = findJMASeriesArea(block.TimeSeries, officeCode, func(area jmaForecastArea) bool {
				return len(area.Pops) > 0 || len(area.WeatherCodes) > 0
			})
		}
	}
	if !found {
		return
	}
	for index, value := range weeklyArea.Pops {
		if value == "" || index >= len(weeklySeries.TimeDefines) {
			continue
		}
		dateTime, err := time.Parse(time.RFC3339, weeklySeries.TimeDefines[index])
		if err != nil {
			continue
		}
		dailyIndex, exists := dailyIndexes[dateTime.Format("2006-01-02")]
		if !exists || report.Daily[dailyIndex].PrecipitationProbability != nil {
			continue
		}
		report.Daily[dailyIndex].PrecipitationProbability = maximumRainChance(map[string]string{"weekly": value})
	}

	for _, series := range block.TimeSeries {
		if len(series.Areas) == 0 || (len(series.Areas[0].TempsMin) == 0 && len(series.Areas[0].TempsMax) == 0) {
			continue
		}
		temperatureIndex := areaIndex
		if temperatureIndex >= len(series.Areas) {
			temperatureIndex = 0
		}
		area := series.Areas[temperatureIndex]
		for index, dateValue := range series.TimeDefines {
			dateTime, err := time.Parse(time.RFC3339, dateValue)
			if err != nil {
				continue
			}
			dailyIndex, exists := dailyIndexes[dateTime.Format("2006-01-02")]
			if !exists {
				continue
			}
			if report.Daily[dailyIndex].TemperatureMin == nil && index < len(area.TempsMin) {
				report.Daily[dailyIndex].TemperatureMin = parseOptionalFloat(&area.TempsMin[index])
			}
			if report.Daily[dailyIndex].TemperatureMax == nil && index < len(area.TempsMax) {
				report.Daily[dailyIndex].TemperatureMax = parseOptionalFloat(&area.TempsMax[index])
			}
		}
		break
	}
}

func jmaDateLabel(index int) string {
	labels := []string{"今日", "明日", "明後日"}
	if index >= 0 && index < len(labels) {
		return labels[index]
	}
	return ""
}

func jmaLocationName(cityCode, areaName string) string {
	prefectures := [...]string{
		"", "北海道", "青森県", "岩手県", "宮城県", "秋田県", "山形県", "福島県",
		"茨城県", "栃木県", "群馬県", "埼玉県", "千葉県", "東京都", "神奈川県",
		"新潟県", "富山県", "石川県", "福井県", "山梨県", "長野県", "岐阜県",
		"静岡県", "愛知県", "三重県", "滋賀県", "京都府", "大阪府", "兵庫県",
		"奈良県", "和歌山県", "鳥取県", "島根県", "岡山県", "広島県", "山口県",
		"徳島県", "香川県", "愛媛県", "高知県", "福岡県", "佐賀県", "長崎県",
		"熊本県", "大分県", "宮崎県", "鹿児島県", "沖縄県",
	}
	areaName = strings.TrimSpace(areaName)
	if len(cityCode) < 2 {
		return areaName
	}
	prefectureCode, err := strconv.Atoi(cityCode[:2])
	if err != nil || prefectureCode <= 0 || prefectureCode >= len(prefectures) {
		return areaName
	}
	prefecture := prefectures[prefectureCode]
	if areaName == "" || strings.HasPrefix(areaName, prefecture) {
		return areaName
	}
	return prefecture + " " + areaName
}

func (c *weatherClient) fillTodayTemperatureFromAmedas(ctx context.Context, stationCode string, today *dailyForecast) error {
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

func jmaOfficeCode(cityCode string) (string, error) {
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

func (c *weatherClient) getLatestHumidity(ctx context.Context, stationCode string) (*int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.jmaLatestEndpoint, nil)
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
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	latestBytes, err := io.ReadAll(io.LimitReader(resp.Body, 128))
	if err != nil {
		return nil, err
	}
	latest, err := time.Parse(time.RFC3339, strings.TrimSpace(string(latestBytes)))
	if err != nil {
		return nil, fmt.Errorf("アメダス最新時刻が不正です: %w", err)
	}
	var observations map[string]struct {
		Humidity []float64 `json:"humidity"`
	}
	endpoint := c.jmaAmedasEndpoint + latest.Format("200601021504") + "00.json"
	if err := c.getJSON(ctx, endpoint, &observations); err != nil {
		return nil, fmt.Errorf("アメダス最新観測値を取得できません: %w", err)
	}
	observation, ok := observations[stationCode]
	if !ok || len(observation.Humidity) == 0 {
		return nil, fmt.Errorf("観測所 %s の湿度がありません", stationCode)
	}
	humidity := int(observation.Humidity[0] + 0.5)
	if humidity < 0 || humidity > 100 {
		return nil, fmt.Errorf("観測所 %s の湿度が不正です", stationCode)
	}
	return &humidity, nil
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
