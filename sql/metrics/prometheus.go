package metrics

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/metrics"
	"github.com/spacemeshos/go-spacemesh/sql"
)

const (
	enabledDBStat = "ENABLE_DBSTAT_VTAB"
	subsystem     = "database" // subsystem shared by all metrics exposed by this package.
)

// DBMetricsCollector collects metrics from db.
type DBMetricsCollector struct {
	logger        log.Logger
	checkInterval time.Duration
	db            *sql.Database
	tablesList    map[string]struct{}
	eg            errgroup.Group
	cancel        context.CancelFunc

	tableSize *prometheus.GaugeVec
	indexSize *prometheus.GaugeVec
	totalSize *prometheus.GaugeVec
}

// NewDBMetricsCollector creates new DBMetricsCollector.
func NewDBMetricsCollector(ctx context.Context, db *sql.Database, logger log.Logger, checkInterval time.Duration) *DBMetricsCollector {
	ctx, cancel := context.WithCancel(ctx)
	collector := &DBMetricsCollector{
		checkInterval: checkInterval,
		logger:        logger.WithName("db_metrics"),
		db:            db,
		cancel:        cancel,
		tableSize:     metrics.NewGauge("table_size", subsystem, "Size of table in bytes", []string{"name"}),
		indexSize:     metrics.NewGauge("index_size", subsystem, "Size of index in bytes", []string{"name"}),
		totalSize:     metrics.NewGauge("total_size", subsystem, "Total size of db in bytes", nil),
	}
	statEnabled, err := collector.checkCompiledWithDBStat()
	if err != nil {
		collector.logger.With().Error("error check compile options", log.Err(err))
		return nil
	}
	if !statEnabled {
		collector.logger.With().Info("sqlite compiled without `SQLITE_ENABLE_DBSTAT_VTAB`. Metrics will not collected")
		return nil
	}

	collector.tablesList, err = collector.getListOfTables()
	if err != nil {
		collector.logger.With().Error("error get list of tables", log.Err(err))
		return nil
	}
	collector.logger.With().Info("start collect stat")
	collector.eg.Go(func() error {
		collector.CollectMetrics(ctx)
		return nil
	})
	return collector
}

// Close closes DBMetricsCollector.
func (d *DBMetricsCollector) Close() {
	d.cancel()
	if err := d.eg.Wait(); err != nil {
		d.logger.With().Error("received error waiting for db metrics collector", log.Err(err))
	}
}

// CollectMetrics collects metrics from db.
func (d *DBMetricsCollector) CollectMetrics(ctx context.Context) {
	ticker := time.NewTicker(d.checkInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			d.logger.Debug("collect stats from db")
			if err := d.collect(); err != nil {
				d.logger.With().Error("error check db metrics", log.Err(err))
			}
		case <-ctx.Done():
			return
		}
	}
}

func (d *DBMetricsCollector) collect() error {
	sizes := make(map[string]int64, 30)
	_, err := d.db.Exec("SELECT name, sum(pgsize) as sum FROM dbstat GROUP BY name", nil, func(stmt *sql.Statement) bool {
		sizes[stmt.ColumnText(0)] = stmt.ColumnInt64(1)
		return true
	})
	if err != nil {
		return errors.Wrap(err, "error execute stat metrics")
	}
	var totalSize int64
	for name, size := range sizes {
		totalSize += size
		_, ok := d.tablesList[name]
		if ok {
			d.tableSize.WithLabelValues(name).Set(float64(size))
			continue
		}
		d.indexSize.WithLabelValues(name).Set(float64(size))
	}
	d.totalSize.WithLabelValues().Set(float64(totalSize))
	return nil
}

func (d *DBMetricsCollector) checkCompiledWithDBStat() (bool, error) {
	var options []string
	_, err := d.db.Exec("PRAGMA compile_options", nil, func(stmt *sql.Statement) bool {
		options = append(options, stmt.ColumnText(0))
		return true
	})
	if err != nil {
		return false, errors.Wrap(err, "error check db compiler options")
	}
	for _, option := range options {
		if option == enabledDBStat {
			return true, nil
		}
	}

	return false, nil
}

// getListOfTables returns list of tables in db. need to separate size indexes and tables.
func (d *DBMetricsCollector) getListOfTables() (map[string]struct{}, error) {
	tables := make(map[string]struct{})
	_, err := d.db.Exec("SELECT name FROM sqlite_master WHERE type='table'", nil, func(stmt *sql.Statement) bool {
		tables[stmt.ColumnText(0)] = struct{}{}
		return true
	})
	if err != nil {
		return nil, errors.Wrap(err, "error get list of tables")
	}
	return tables, nil
}
