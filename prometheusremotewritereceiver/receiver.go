// Copyright  The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package prometheusremotewritereceiver

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"

	"github.com/prometheus/prometheus/storage/remote"
	"github.com/rokoucha/otelcol-prometheus-remote-write-receiver/prometheusremotewritereceiver/translator"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/confighttp"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/receiver/receiverhelper"
	"go.uber.org/zap"
)

const (
	receiverFormat = "protobuf"
)

//var reg = regexp.MustCompile(`(\w+)_(\w+)_(\w+)\z`)

// PrometheusRemoteWriteReceiver - remote write
type PrometheusRemoteWriteReceiver struct {
	config        *Config
	logger        *zap.Logger
	nextConsumer  consumer.Metrics
	obsrecv       *receiverhelper.ObsReport
	server        *http.Server
	settings      receiver.CreateSettings
	shutdownWG    sync.WaitGroup
	timeThreshold *int64
}

// NewReceiver - remote write
func NewReceiver(settings receiver.CreateSettings, config *Config, consumer consumer.Metrics) (*PrometheusRemoteWriteReceiver, error) {
	obsrecv, err := receiverhelper.NewObsReport(receiverhelper.ObsReportSettings{
		ReceiverID:             settings.ID,
		Transport:              "http",
		ReceiverCreateSettings: settings,
	})
	zr := &PrometheusRemoteWriteReceiver{
		settings:      settings,
		nextConsumer:  consumer,
		config:        config,
		logger:        settings.Logger,
		obsrecv:       obsrecv,
		timeThreshold: &config.TimeThreshold,
	}
	return zr, err
}

// Start - remote write
func (r *PrometheusRemoteWriteReceiver) Start(ctx context.Context, host component.Host) error {
	if host == nil {
		return errors.New("nil host")
	}

	listener, err := r.config.ServerConfig.ToListener(ctx)
	if err != nil {
		return err
	}

	handler := http.HandlerFunc(r.handleWrite)

	r.server, err = r.config.ServerConfig.ToServer(ctx, host, r.settings.TelemetrySettings, handler, confighttp.WithDecoder("snappy", func(body io.ReadCloser) (io.ReadCloser, error) { return body, nil }))
	if err != nil {
		return err
	}

	r.shutdownWG.Add(1)
	go func() {
		defer r.shutdownWG.Done()
		if errHTTP := r.server.Serve(listener); !errors.Is(errHTTP, http.ErrServerClosed) && errHTTP != nil {
			r.settings.TelemetrySettings.ReportStatus(component.NewFatalErrorEvent(errHTTP))
		}
	}()

	return nil
}

func (rec *PrometheusRemoteWriteReceiver) handleWrite(w http.ResponseWriter, r *http.Request) {
	ctx := rec.obsrecv.StartMetricsOp(r.Context())
	req, err := remote.DecodeWriteRequest(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	pms, err := translator.FromTimeSeries(req.Timeseries, translator.Settings{
		TimeThreshold: *rec.timeThreshold,
		Logger:        *rec.logger,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	metricCount := pms.ResourceMetrics().Len()
	dataPointCount := pms.DataPointCount()
	if metricCount != 0 {
		err = rec.nextConsumer.ConsumeMetrics(ctx, pms)
	}
	rec.obsrecv.EndMetricsOp(ctx, receiverFormat, dataPointCount, err)
	w.WriteHeader(http.StatusAccepted)
}

// Shutdown - remote write
func (rec *PrometheusRemoteWriteReceiver) Shutdown(context.Context) error {
	err := rec.server.Close()
	rec.shutdownWG.Wait()
	return err
}
