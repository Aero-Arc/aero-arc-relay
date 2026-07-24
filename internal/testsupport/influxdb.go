//go:build integration

package testsupport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const InfluxDBImage = "influxdb:3.10.3-core"

type InfluxDB struct {
	URL      string
	Database string
	Token    string
	Client   *influxdb3.Client
}

func (i *InfluxDB) QueryRows(ctx context.Context, query string) ([]map[string]any, error) {
	iterator, err := i.Client.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	var rows []map[string]any
	for iterator.Next() {
		rows = append(rows, iterator.Value())
	}
	if err := iterator.Err(); err != nil {
		return rows, err
	}
	return rows, nil
}

func (i *InfluxDB) AwaitRow(ctx context.Context, interval time.Duration, query, identity string) (map[string]any, error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var lastErr error
	var lastRows []map[string]any
	for {
		lastRows, lastErr = i.QueryRows(ctx, query)
		if lastErr == nil && len(lastRows) > 0 {
			return lastRows[0], nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf(
				"await InfluxDB row for %s in database %s: %w (last query error: %v; last result: %#v)",
				identity, i.Database, ctx.Err(), lastErr, lastRows,
			)
		case <-ticker.C:
		}
	}
}

func StartInfluxDB(t *testing.T) *InfluxDB {
	t.Helper()
	testcontainers.SkipIfProviderIsNotHealthy(t)

	// Include enough time for a cold image pull before the wait strategy gets
	// its own bounded 90-second readiness window. The suite timeout is 10m.
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	t.Logf(
		"Starting InfluxDB test dependency: image=%s (Testcontainers may also log its separate Ryuk cleanup-helper container)",
		InfluxDBImage,
	)
	container, err := testcontainers.Run(ctx, InfluxDBImage,
		testcontainers.WithExposedPorts("8181/tcp"),
		testcontainers.WithCmd(
			"influxdb3", "serve",
			"--node-id=integration-test",
			"--object-store=memory",
			"--without-auth",
		),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/health").
				WithPort("8181/tcp").
				WithStartupTimeout(90*time.Second).
				WithPollInterval(250*time.Millisecond),
		),
	)
	if err != nil {
		t.Fatalf("start %s: %v", InfluxDBImage, err)
	}
	containerID := shortContainerID(container.GetContainerID())
	t.Cleanup(func() {
		if t.Failed() {
			logCtx, logCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer logCancel()
			if logs, logErr := container.Logs(logCtx); logErr == nil {
				body, _ := io.ReadAll(logs)
				t.Logf("InfluxDB container logs:\n%s", body)
			} else {
				t.Logf("read InfluxDB container logs: %v", logErr)
			}
		}
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer stopCancel()
		t.Logf("Stopping InfluxDB test dependency: container_id=%s image=%s", containerID, InfluxDBImage)
		if err := testcontainers.TerminateContainer(container, testcontainers.StopContext(stopCtx)); err != nil {
			t.Errorf("terminate InfluxDB container: %v", err)
			return
		}
		t.Logf("InfluxDB test dependency stopped: container_id=%s", containerID)
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("resolve InfluxDB container host: %v", err)
	}
	port, err := container.MappedPort(ctx, "8181/tcp")
	if err != nil {
		t.Fatalf("resolve InfluxDB mapped port: %v", err)
	}
	instance := &InfluxDB{
		URL:      fmt.Sprintf("http://%s:%s", host, port.Port()),
		Database: uniqueDatabaseName(t.Name()),
		// The production client requires a non-empty token. InfluxDB is isolated
		// inside Testcontainers and started without auth, so this is not a secret.
		Token: "integration-test-token",
	}
	createDatabase(t, instance)

	instance.Client, err = influxdb3.New(influxdb3.ClientConfig{
		Host:     instance.URL,
		Token:    instance.Token,
		Database: instance.Database,
		Timeout:  5 * time.Second,
	})
	if err != nil {
		t.Fatalf("create InfluxDB query client: %v", err)
	}
	t.Cleanup(func() {
		if err := instance.Client.Close(); err != nil {
			t.Errorf("close InfluxDB query client: %v", err)
		}
	})
	t.Logf(
		"InfluxDB test dependency ready: container_id=%s image=%s endpoint=%s database=%s",
		containerID, InfluxDBImage, instance.URL, instance.Database,
	)
	return instance
}

func createDatabase(t *testing.T, instance *InfluxDB) {
	t.Helper()
	body, err := json.Marshal(map[string]string{"db": instance.Database})
	if err != nil {
		t.Fatalf("encode InfluxDB database request: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, instance.URL+"/api/v3/configure/database", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build InfluxDB database request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create InfluxDB database %q at %s: %v", instance.Database, instance.URL, err)
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create InfluxDB database %q: status=%s body=%s", instance.Database, resp.Status, responseBody)
	}
}

func uniqueDatabaseName(_ string) string {
	// InfluxDB 3 limits database names to 64 characters.
	return fmt.Sprintf("relay-it-%d", time.Now().UnixNano())
}

func shortContainerID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
