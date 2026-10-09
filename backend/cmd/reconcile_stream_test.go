package main

import (
	"bytes"
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"sync/atomic"
	"testing"

	"gorm.io/gorm"
)

// Generate rows on demand so the fixture cannot conceal corpus buffering.
type reconciliationTestRows struct {
	ctx               context.Context
	total             int
	generated, closed atomic.Int32
	row               func(int) []driver.Value
	failAt            int
	failure, closeErr error
}

func (*reconciliationTestRows) Columns() []string {
	return []string{"id", "content", "kind", "confidence", "tags"}
}

func (r *reconciliationTestRows) Close() error {
	r.closed.Add(1)
	return r.closeErr
}

func (r *reconciliationTestRows) Next(dest []driver.Value) error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	i := int(r.generated.Load())
	if r.failure != nil && i >= r.failAt {
		return r.failure
	}
	if i >= r.total {
		return io.EOF
	}
	row := r.row(i)
	if len(row) != len(dest) {
		return fmt.Errorf("synthetic row has %d columns; expected %d", len(row), len(dest))
	}
	copy(dest, row)
	r.generated.Add(1)
	return nil
}

func reconciliationFixtureRow(i int) []driver.Value {
	return []driver.Value{fmt.Sprintf("00000000-0000-0000-0000-%012d", i+1), "stored source", "fact", float64(2), "source"}
}

type reconciliationWriterFunc func([]byte) (int, error)

func (f reconciliationWriterFunc) Write(p []byte) (int, error) { return f(p) }

func executeStreamingFixture(t *testing.T, connector *commandTestConnector, rows *reconciliationTestRows, output io.Writer, wrap func(*gorm.DB) *gorm.DB) (reconciliationSummary, error) {
	t.Helper()
	t.Setenv("HAI_COMMAND_TIMEOUT", "10s")
	connector.memoryRows = func(ctx context.Context) driver.Rows {
		rows.ctx = ctx
		return rows
	}
	var finalizedBeforeClose atomic.Bool
	connector.beforeFinalize = func() {
		if rows.closed.Load() != 1 {
			finalizedBeforeClose.Store(true)
		}
	}
	db, pool := commandTestDatabase(t, connector)
	var summary reconciliationSummary
	var scanErr error
	code := withCommandDatabase("reconcile", func(context.Context) (*gorm.DB, error) { return db, nil }, func(bound *gorm.DB) int {
		if wrap != nil {
			bound = wrap(bound)
		}
		summary, scanErr = streamReconcile(bound, output)
		if scanErr != nil {
			return 1
		}
		return 0
	})
	if (code != 0) != (scanErr != nil) {
		t.Fatalf("exit=%d, streaming error=%v", code, scanErr)
	}
	if !connector.readOnly.Load() || connector.begun.Load() != 1 || connector.queried.Load() != 1 || connector.executed.Load() != 0 {
		t.Fatal("scan must use one read-only transaction/query and no mutations")
	}
	if rows.closed.Load() != 1 {
		t.Fatalf("driver rows closed %d times", rows.closed.Load())
	}
	if finalizedBeforeClose.Load() {
		t.Fatal("transaction finalized before its rows closed")
	}
	assertCommandPoolClosed(t, pool, connector)
	return summary, scanErr
}

func TestReconciliationFixtureRejectsIncompleteRows(t *testing.T) {
	rows := &reconciliationTestRows{ctx: context.Background(), total: 2, row: func(i int) []driver.Value {
		row := reconciliationFixtureRow(i)
		if i == 1 {
			return row[:4]
		}
		return row
	}}
	dest := make([]driver.Value, 5)
	if err := rows.Next(dest); err != nil {
		t.Fatal(err)
	}
	if err := rows.Next(dest); err == nil || !strings.Contains(err.Error(), "synthetic row has 4 columns; expected 5") || rows.generated.Load() != 1 {
		t.Fatalf("short fixture row was accepted: generated=%d err=%v", rows.generated.Load(), err)
	}
}

func TestReconcileStreamsLargeCorpusBeforeReadingNextRow(t *testing.T) {
	connector := &commandTestConnector{}
	rows := &reconciliationTestRows{total: 5000, row: func(i int) []driver.Value {
		row := reconciliationFixtureRow(i)
		row[1] = strings.Repeat("source material ", 1024)
		return row
	}}
	findings, headers, summaries := 0, 0, 0
	output := reconciliationWriterFunc(func(p []byte) (int, error) {
		line := string(p)
		switch {
		case strings.HasPrefix(line, "- "):
			findings++
			if int(rows.generated.Load()) != findings || rows.closed.Load() != 0 || connector.committed.Load() != 0 {
				t.Error("finding was buffered instead of emitted during the scan")
			}
			if strings.Contains(line, "source material") {
				t.Error("finding leaked stored source content")
			}
		case strings.HasPrefix(line, "reconcile: provisional"):
			headers++
			if rows.generated.Load() != 1 {
				t.Error("provisional header was not emitted with the first finding")
			}
		case strings.HasPrefix(line, "reconcile: scanned"):
			summaries++
			if int(rows.generated.Load()) != rows.total || rows.closed.Load() != 1 || connector.committed.Load() != 1 {
				t.Error("completion was emitted before rows closed and transaction committed")
			}
		default:
			t.Errorf("unexpected output: %q", line)
		}
		return len(p), nil
	})
	summary, err := executeStreamingFixture(t, connector, rows, output, nil)
	if err != nil || summary.scanned != rows.total || summary.findings != rows.total || findings != rows.total || headers != 1 || summaries != 1 {
		t.Fatalf("incomplete streaming corpus: summary=%+v findings=%d headers=%d summaries=%d err=%v", summary, findings, headers, summaries, err)
	}
}

func TestReconcileDoesNotCarryFieldsAcrossNullableRows(t *testing.T) {
	rows := &reconciliationTestRows{total: 2, row: func(i int) []driver.Value {
		row := reconciliationFixtureRow(i)
		row[3] = float64(0.5)
		if i == 1 {
			row[1], row[2], row[4] = nil, nil, nil
		}
		return row
	}}
	var output bytes.Buffer
	summary, err := executeStreamingFixture(t, &commandTestConnector{}, rows, &output, nil)
	if err != nil || summary.scanned != 2 || summary.findings != 1 {
		t.Fatalf("nullable row carried prior data: summary=%+v err=%v", summary, err)
	}
	if !strings.Contains(output.String(), "00000000-0000-0000-0000-000000000002 repairable=false") || !strings.Contains(output.String(), "fill missing required fields") {
		t.Fatalf("missing manual finding for nullable fields: %s", &output)
	}
}

func TestReconcileDistinguishesNullConfidenceFromZero(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprintf("missing=%t", missing), func(t *testing.T) {
			connector := &commandTestConnector{}
			rows := &reconciliationTestRows{total: 2, row: func(i int) []driver.Value {
				row := reconciliationFixtureRow(i)
				row[3] = float64(0)
				if missing && i == 1 {
					row[3] = nil
				}
				return row
			}}
			var output bytes.Buffer
			summary, err := executeStreamingFixture(t, connector, rows, &output, nil)
			if missing {
				if err == nil || !strings.Contains(err.Error(), "00000000-0000-0000-0000-000000000002: confidence is NULL") ||
					!strings.Contains(err.Error(), "verified source information") || summary.scanned != 1 || summary.findings != 0 ||
					connector.committed.Load() != 0 || connector.rolledBack.Load() != 1 || output.Len() != 0 {
					t.Fatalf("missing confidence reported clean: summary=%+v err=%v output=%q", summary, err, output.String())
				}
			} else if err != nil || summary.scanned != 2 || summary.findings != 0 || !strings.Contains(output.String(), "all memories satisfy") {
				t.Fatalf("valid zero confidence refused: summary=%+v err=%v", summary, err)
			}
		})
	}
}

func TestReconcileFailureNeverEmitsCompletion(t *testing.T) {
	failure := errors.New("synthetic streaming failure")
	for _, scenario := range []string{"late_read", "scan", "rows_close", "commit", "header_write", "finding_write", "short_write"} {
		t.Run(scenario, func(t *testing.T) {
			connector := &commandTestConnector{}
			rows := &reconciliationTestRows{total: 3, row: reconciliationFixtureRow}
			var output bytes.Buffer
			var writer io.Writer = &output
			want := failure
			wantScanned, wantFindings := 3, 3
			switch scenario {
			case "late_read":
				rows.failure, rows.failAt = failure, 2
				wantScanned, wantFindings = 2, 2
			case "scan":
				rows.row = func(i int) []driver.Value {
					row := reconciliationFixtureRow(i)
					row[3] = "not a numeric confidence"
					return row
				}
				wantScanned, wantFindings = 0, 0
				want = nil // The actual conversion error originates in database/sql.
			case "rows_close":
				rows.closeErr = failure
			case "commit":
				connector.commitFailure = failure
			case "header_write", "finding_write", "short_write":
				wantScanned, wantFindings = 1, 1
				if scenario == "short_write" {
					want = io.ErrShortWrite
				}
				writer = reconciliationWriterFunc(func(p []byte) (int, error) {
					if scenario != "finding_write" || strings.HasPrefix(string(p), "- ") {
						if scenario == "short_write" {
							return len(p) - 1, nil
						}
						return 0, failure
					}
					return output.Write(p)
				})
			}
			summary, err := executeStreamingFixture(t, connector, rows, writer, nil)
			if err == nil || (want != nil && !errors.Is(err, want)) || summary.scanned != wantScanned || summary.findings != wantFindings {
				t.Fatalf("incorrect failure/counters: summary=%+v err=%v want=%v", summary, err, want)
			}
			if strings.Contains(output.String(), "reconcile: scanned") || strings.Contains(output.String(), "all memories satisfy") {
				t.Fatalf("failed scan claimed completion: %s", &output)
			}
			if scenario == "commit" {
				if connector.committed.Load() != 1 {
					t.Fatal("commit failure fixture was not exercised")
				}
			} else if connector.committed.Load() != 0 || connector.rolledBack.Load() != 1 {
				t.Fatal("failed scan committed or retained its transaction")
			}
		})
	}
}

func TestReconcileCancellationDoesNotClaimCleanResult(t *testing.T) {
	for _, when := range []string{"finding", "summary", "clean_result"} {
		t.Run(when, func(t *testing.T) {
			connector := &commandTestConnector{}
			rows := &reconciliationTestRows{row: reconciliationFixtureRow}
			if when == "finding" {
				rows.total = 3
			}
			var cancel context.CancelFunc
			var output bytes.Buffer
			writer := reconciliationWriterFunc(func(p []byte) (int, error) {
				line := string(p)
				if (when == "finding" && strings.HasPrefix(line, "- ")) ||
					(when == "summary" && strings.HasPrefix(line, "reconcile: scanned")) ||
					(when == "clean_result" && strings.Contains(line, "all memories satisfy")) {
					cancel()
				}
				return output.Write(p)
			})
			_, err := executeStreamingFixture(t, connector, rows, writer, func(db *gorm.DB) *gorm.DB {
				var ctx context.Context
				ctx, cancel = context.WithCancel(db.Statement.Context)
				t.Cleanup(cancel)
				return db.WithContext(ctx)
			})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled output returned success: %v", err)
			}
			if when != "clean_result" && strings.Contains(output.String(), "all memories satisfy") {
				t.Fatalf("canceled scan emitted a clean-result line: %s", &output)
			}
			if when == "finding" && strings.Contains(output.String(), "reconcile: scanned") {
				t.Fatal("canceled scan emitted a final summary")
			}
		})
	}
}

func TestReconcileZeroRowsCompletesOnlyAfterCommit(t *testing.T) {
	connector := &commandTestConnector{}
	rows := &reconciliationTestRows{row: reconciliationFixtureRow}
	var output bytes.Buffer
	writer := reconciliationWriterFunc(func(p []byte) (int, error) {
		if connector.committed.Load() != 1 || rows.closed.Load() != 1 {
			t.Error("clean output preceded row closure/commit")
		}
		return output.Write(p)
	})
	summary, err := executeStreamingFixture(t, connector, rows, writer, nil)
	want := "reconcile: scanned 0 memories, 0 finding(s)\nreconcile: all memories satisfy their invariants\n"
	if err != nil || summary != (reconciliationSummary{}) || output.String() != want {
		t.Fatalf("zero-row output: summary=%+v err=%v output=%q", summary, err, output.String())
	}
}

func TestReconcileNonFiniteConfidenceRequiresManualReview(t *testing.T) {
	for _, confidence := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		rows := &reconciliationTestRows{total: 1, row: func(i int) []driver.Value {
			row := reconciliationFixtureRow(i)
			row[3] = confidence
			return row
		}}
		var output bytes.Buffer
		summary, err := executeStreamingFixture(t, &commandTestConnector{}, rows, &output, nil)
		if err != nil || summary.findings != 1 || !strings.Contains(output.String(), "repairable=false") || !strings.Contains(output.String(), "verified source information") || strings.Contains(output.String(), "clamp") {
			t.Fatalf("non-finite confidence got an unsafe repair: summary=%+v err=%v output=%q", summary, err, output.String())
		}
	}
}

func TestReconcileOutputFailureAfterCommitStillFails(t *testing.T) {
	for _, when := range []string{"summary", "clean_result"} {
		t.Run(when, func(t *testing.T) {
			connector := &commandTestConnector{}
			rows := &reconciliationTestRows{row: reconciliationFixtureRow}
			failure := errors.New("synthetic output failure")
			var output bytes.Buffer
			writer := reconciliationWriterFunc(func(p []byte) (int, error) {
				if when == "summary" || strings.Contains(string(p), "all memories satisfy") {
					return 0, failure
				}
				return output.Write(p)
			})
			_, err := executeStreamingFixture(t, connector, rows, writer, nil)
			if !errors.Is(err, failure) || connector.committed.Load() != 1 || strings.Contains(output.String(), "all memories satisfy") {
				t.Fatalf("output failure lost: err=%v commits=%d output=%q", err, connector.committed.Load(), output.String())
			}
		})
	}
}
