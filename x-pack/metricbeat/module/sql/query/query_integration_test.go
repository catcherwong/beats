// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

//go:build integration
// +build integration

package query

import (
	"fmt"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Drivers
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"

	"github.com/elastic/beats/v7/libbeat/beat"
	"github.com/elastic/beats/v7/libbeat/tests/compose"
	"github.com/elastic/beats/v7/metricbeat/mb"
	mbtest "github.com/elastic/beats/v7/metricbeat/mb/testing"
	"github.com/elastic/beats/v7/metricbeat/module/mysql"
	"github.com/elastic/beats/v7/metricbeat/module/postgresql"
)

type testFetchConfig struct {
	config    config
	Host      string
	Assertion func(t *testing.T, event beat.Event)
}

func TestMySQL(t *testing.T) {
	service := compose.EnsureUp(t, "mysql")
	cfg := testFetchConfig{
		config: config{
			Driver:         "mysql",
			Query:          "select table_schema, table_name, engine, table_rows from information_schema.tables where table_rows > 0;",
			ResponseFormat: tableResponseFormat,

			RawData: rawData{
				Enabled: true,
			},
		},
		Host:      mysql.GetMySQLEnvDSN(service.Host()),
		Assertion: assertFieldNotContains("service.address", ":test@"),
	}

	t.Run("fetch", func(t *testing.T) {
		testFetch(t, cfg)
	})

	t.Run("data", func(t *testing.T) {
		testData(t, cfg, "./_meta/data_mysql_tables.json")
	})

	cfg = testFetchConfig{
		config: config{
			Driver:         "mysql",
			Query:          "show status;",
			ResponseFormat: variableResponseFormat,
			RawData: rawData{
				Enabled: true,
			},
		},
		Host:      mysql.GetMySQLEnvDSN(service.Host()),
		Assertion: assertFieldNotContains("service.address", ":test@"),
	}

	t.Run("fetch", func(t *testing.T) {
		testFetch(t, cfg)
	})

	t.Run("data", func(t *testing.T) {
		testData(t, cfg, "./_meta/data_mysql_variables.json")
	})
}

func TestPostgreSQL(t *testing.T) {
	service := compose.EnsureUp(t, "postgresql")
	host, port, err := net.SplitHostPort(service.Host())
	require.NoError(t, err)

	user := postgresql.GetEnvUsername()
	password := postgresql.GetEnvPassword()

	cfg := testFetchConfig{
		config: config{
			Driver:         "postgres",
			Query:          "select * from pg_stat_database",
			ResponseFormat: tableResponseFormat,
		},
		Host:      fmt.Sprintf("user=%s password=%s sslmode=disable host=%s port=%s", user, password, host, port),
		Assertion: assertFieldNotContains("service.address", "password="+password),
	}

	t.Run("fetch", func(t *testing.T) {
		testFetch(t, cfg)
	})

	cfg = testFetchConfig{
		config: config{
			Driver:         "postgres",
			Query:          "select * from pg_stat_database where datname='postgres'",
			ResponseFormat: tableResponseFormat,
			RawData: rawData{
				Enabled: true,
			},
		},
		Host:      fmt.Sprintf("postgres://%s:%s@%s:%s/?sslmode=disable", user, password, host, port),
		Assertion: assertFieldNotContains("service.address", ":"+password+"@"),
	}

	t.Run("fetch with URL", func(t *testing.T) {
		testFetch(t, cfg)
	})

	t.Run("data", func(t *testing.T) {
		testData(t, cfg, "./_meta/data_postgres_tables.json")
	})

	cfg = testFetchConfig{
		config: config{
			Driver:         "postgres",
			Query:          "select name, setting from pg_settings",
			ResponseFormat: variableResponseFormat,
			RawData: rawData{
				Enabled: true,
			},
		},
		Host:      fmt.Sprintf("postgres://%s:%s@%s:%s/?sslmode=disable", user, password, host, port),
		Assertion: assertFieldNotContains("service.address", ":"+password+"@"),
	}

	t.Run("fetch with URL", func(t *testing.T) {
		testFetch(t, cfg)
	})

	t.Run("data", func(t *testing.T) {
		testData(t, cfg, "./_meta/data_postgres_variables.json")
	})

	t.Run("raw_data", func(t *testing.T) {
		t.Run("variable mode", func(t *testing.T) {
			cfg = testFetchConfig{
				config: config{
					Driver:         "postgres",
					Query:          "select name, setting from pg_settings",
					ResponseFormat: variableResponseFormat,
					RawData: rawData{
						Enabled: true,
					},
				},
				Host: fmt.Sprintf("postgres://%s:%s@%s:%s/?sslmode=disable", user, password, host, port),
				Assertion: func(t *testing.T, event beat.Event) {
					value, err := event.GetValue("sql.query")
					assert.NoError(t, err)
					require.NotEmpty(t, value.(map[string]interface{}))
				},
			}

		})

		t.Run("table mode", func(t *testing.T) {
			cfg = testFetchConfig{
				config: config{
					Driver:         "postgres",
					Query:          "select * from pg_settings",
					ResponseFormat: tableResponseFormat,
					RawData: rawData{
						Enabled: true,
					},
				},
				Host: fmt.Sprintf("postgres://%s:%s@%s:%s/?sslmode=disable", user, password, host, port),
				Assertion: func(t *testing.T, event beat.Event) {
					value, err := event.GetValue("sql.query")
					assert.NoError(t, err)
					require.NotEmpty(t, value.(map[string]interface{}))
				},
			}

		})
	})
}

func testFetch(t *testing.T, cfg testFetchConfig) {
	m := mbtest.NewFetcher(t, getConfig(cfg))
	events, errs := m.FetchEvents()
	require.Empty(t, errs)
	require.NotEmpty(t, events)
	t.Logf("%s/%s event: %+v", m.Module().Name(), m.Name(), events[0])

	if cfg.Assertion != nil {
		for _, event := range events {
			cfg.Assertion(t, m.StandardizeEvent(event, mb.AddMetricSetInfo))
		}
	}
}

func testData(t *testing.T, cfg testFetchConfig, postfix string) {
	m := mbtest.NewFetcher(t, getConfig(cfg))
	m.WriteEvents(t, postfix)
}

func getConfig(cfg testFetchConfig) map[string]interface{} {
	values := map[string]interface{}{
		"module":           "sql",
		"metricsets":       []string{"query"},
		"hosts":            []string{cfg.Host},
		"driver":           cfg.config.Driver,
		"sql_query":        cfg.config.Query,
		"raw_data.enabled": cfg.config.RawData.Enabled,
	}

	if cfg.config.ResponseFormat != "" {
		values["sql_response_format"] = cfg.config.ResponseFormat
	}

	return values
}

func assertFieldNotContains(field, s string) func(t *testing.T, event beat.Event) {
	return func(t *testing.T, event beat.Event) {
		value, err := event.GetValue(field)
		assert.NoError(t, err)
		require.NotEmpty(t, value.(string))
		require.NotContains(t, value.(string), s)
	}
}
