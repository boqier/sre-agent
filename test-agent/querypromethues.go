package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	prometheusURL = "http://192.168.30.11:30090" // 替换成你的Prometheus NodePort IP和端口
)

// --- 数据结构定义 ---

// Prometheus告警响应结构体
type PrometheusAlerts struct {
	Status string `json:"status"`
	Data   struct {
		Alerts []Alert `json:"alerts"`
	} `json:"data"`
}

// 单个告警的结构体
type Alert struct {
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	State       string            `json:"state"`
	ActiveAt    time.Time         `json:"activeAt"`
}

func queryPrometheusAlerts() ([]Alert, error) {
	resp, err := http.Get(prometheusURL + "/api/v1/alerts")
	if err != nil {
		return nil, fmt.Errorf("failed to get alerts from Prometheus: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus returned non-200 status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var alertResponse PrometheusAlerts
	if err := json.Unmarshal(body, &alertResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal json: %w", err)
	}

	var firingAlerts []Alert
	for _, alert := range alertResponse.Data.Alerts {
		if alert.State == "firing" {
			firingAlerts = append(firingAlerts, alert)
		}
	}
	return firingAlerts, nil
}
