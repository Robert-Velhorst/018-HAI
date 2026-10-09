//go:build integration

package source

import (
	"context"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/pursuit"
	"automation-hub-backend/internal/workflow"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTrelloPursuitWorkflowLifecyclePostgres(t *testing.T) {
	db := isolatedPursuitLifecyclePostgresDB(t)

	cards := `[{"id":"000000000000000000000064","name":"Prepare evidence packet","desc":"Review source links and prepare a concise evidence packet for the hearing.","shortUrl":"https://trello.com/c/000000000000000000000064","dateLastActivity":"2026-09-20T10:00:00Z","idList":"list-1","checklists":[{"id":"checklist-1","name":"Evidence","pos":1,"checkItems":[{"id":"open-item","name":"Verify source links and prepare a reviewable packet","state":"incomplete","pos":1}]}]}]`
	server, _, _ := trelloTestServer(t, cards)
	defer server.Close()
	configureTrelloTest(t, server.URL)

	owner := "trello-lifecycle-" + uuid.NewString()
	t.Setenv(trelloOwnerIdentityEnv, owner)
	sourceRecord := newTrelloSource(uuid.New(), "abc123XY", "")
	sourceRecord.OwnerIdentity = owner
	sourceRecord.DefaultProjectKey = "board-project-unverified"
	if err := db.Create(sourceRecord).Error; err != nil {
		t.Fatalf("create connected Trello source: %v", err)
	}

	workflowService := workflow.NewService(workflow.NewGormRepository(db))
	pursuitRepository := &pursuit.GormRepository{DB: db}
	pursuitService := pursuit.NewService(pursuitRepository, workflowService)
	sourceService := NewServiceWithWorkflowAndPursuitLinker(
		NewGormRepository(db), nil, workflowService, pursuitService,
	)

	firstSync, firstExtractions, firstPursuitOutcomes := syncTrelloUntilCompleted(t, sourceService, sourceRecord.ID)
	if firstSync.Job.Status != "completed" || len(firstExtractions) != 1 || len(firstPursuitOutcomes) != 1 {
		t.Fatalf("first sync status=%q extractions=%d pursuit outcomes=%#v; want one completed actionable intake", firstSync.Job.Status, len(firstExtractions), firstPursuitOutcomes)
	}
	outcome := firstPursuitOutcomes[0]
	if outcome.Status != "candidate_pending" || outcome.PursuitID == "" || outcome.WorkflowID != "" {
		t.Fatalf("first pursuit outcome = %#v; want review candidate and no workflow before acceptance", outcome)
	}
	pursuitID, err := uuid.Parse(outcome.PursuitID)
	if err != nil {
		t.Fatalf("parse candidate pursuit ID: %v", err)
	}
	var candidate models.Pursuit
	if err := db.First(&candidate, "id = ? AND owner_identity = ?", pursuitID, owner).Error; err != nil {
		t.Fatalf("load persisted pursuit candidate: %v", err)
	}
	if candidate.ProjectKey != "" {
		t.Fatalf("unverified Trello project hint was promoted to ProjectKey %q", candidate.ProjectKey)
	}
	if !strings.Contains(candidate.Description, "Unverified project hint from the source: board-project-unverified") || !strings.Contains(candidate.Description, "has not been confirmed") {
		t.Fatalf("persisted candidate lost the unverified project hint: %q", candidate.Description)
	}
	if got := countOwnerRows(t, db, &models.WorkflowItem{}, owner); got != 0 {
		t.Fatalf("workflows before candidate acceptance = %d; want zero", got)
	}
	if got := countOwnerRows(t, db, &models.Pursuit{}, owner); got != 1 {
		t.Fatalf("persisted pursuit candidates after first sync = %d; want one", got)
	}
	if got := countSourceRows(t, db, &models.SourceRawItem{}, sourceRecord.ID); got != 1 {
		t.Fatalf("persisted Trello raw items = %d; want one", got)
	}
	if got := countSourceRows(t, db, &models.SourceExtraction{}, sourceRecord.ID); got != 1 {
		t.Fatalf("persisted Trello extractions = %d; want one", got)
	}

	links, err := pursuitRepository.FindLinks(pursuitID)
	if err != nil {
		t.Fatalf("load candidate source links: %v", err)
	}
	if !hasPursuitTestLink(mapPursuitLinks(links), pursuitID, pursuit.LinkSourceItem) || !hasPursuitTestLink(mapPursuitLinks(links), pursuitID, pursuit.LinkSourceExtraction) {
		t.Fatalf("candidate is missing raw-item or extraction provenance links: %#v", links)
	}

	replaySync, _, _ := syncTrelloUntilCompleted(t, sourceService, sourceRecord.ID)
	if replaySync.Job.ID == firstSync.Job.ID {
		t.Fatalf("completed Trello cycle reused logical job %s; want a new cycle job", replaySync.Job.ID)
	}
	if got := countSourceRows(t, db, &models.SourceSyncJob{}, sourceRecord.ID); got != 2 {
		t.Fatalf("persisted Trello sync cycles = %d; want two distinct completed cycles", got)
	}
	if got := countOwnerRows(t, db, &models.Pursuit{}, owner); got != 1 {
		t.Fatalf("pursuit candidates after replay = %d; want the same single candidate", got)
	}
	if got := countSourceRows(t, db, &models.SourceRawItem{}, sourceRecord.ID); got != 1 {
		t.Fatalf("raw items after replay = %d; want one", got)
	}
	if got := countSourceRows(t, db, &models.SourceExtraction{}, sourceRecord.ID); got != 1 {
		t.Fatalf("extractions after replay = %d; want one", got)
	}
	if got := countOwnerRows(t, db, &models.WorkflowItem{}, owner); got != 0 {
		t.Fatalf("replay created %d workflow(s) before candidate acceptance", got)
	}

	if _, err := pursuitService.AcceptCandidateForOwner(owner, pursuitID, pursuit.PlanRequest{Actor: owner}); err != nil {
		t.Fatalf("explicit candidate acceptance: %v", err)
	}
	var createdWorkflows []models.WorkflowItem
	if err := db.Where("owner_identity = ?", owner).Find(&createdWorkflows).Error; err != nil {
		t.Fatalf("load workflows after acceptance: %v", err)
	}
	if len(createdWorkflows) != 1 {
		t.Fatalf("persisted workflows after acceptance = %d; want exactly one", len(createdWorkflows))
	}
	created := createdWorkflows[0]
	if created.SourceType != pursuit.LinkPursuit || created.SourceID != pursuitID.String() || created.SourceURI != "pursuit://"+pursuitID.String() {
		t.Fatalf("accepted workflow lost pursuit provenance: %#v", created)
	}
	links, err = pursuitRepository.FindLinks(pursuitID)
	if err != nil {
		t.Fatalf("load accepted pursuit links: %v", err)
	}
	linkedWorkflow := false
	for _, link := range links {
		if link.LinkType == pursuit.LinkWorkflow && link.LinkID == created.ID.String() {
			linkedWorkflow = true
			break
		}
	}
	if !linkedWorkflow {
		t.Fatalf("accepted workflow %s was not persisted as a pursuit link: %#v", created.ID, links)
	}
}

func syncTrelloUntilCompleted(t *testing.T, service Service, sourceID uuid.UUID) (*SyncResult, []models.SourceExtraction, []PursuitRoutingOutcome) {
	t.Helper()
	const maxPages = 20
	var (
		lastResult  *SyncResult
		extractions []models.SourceExtraction
		outcomes    []PursuitRoutingOutcome
	)
	for page := 0; page < maxPages; page++ {
		result, err := service.Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
		if err != nil {
			t.Fatalf("Trello sync page %d: %v", page+1, err)
		}
		if result == nil {
			t.Fatalf("Trello sync page %d returned no result", page+1)
		}
		lastResult = result
		extractions = append(extractions, result.Extractions...)
		outcomes = append(outcomes, result.PursuitOutcomes...)
		switch result.Job.Status {
		case "completed":
			return lastResult, extractions, outcomes
		case "running":
			continue
		default:
			t.Fatalf("Trello sync page %d ended in %q: %s", page+1, result.Job.Status, result.Job.Message)
		}
	}
	t.Fatalf("Trello sync did not complete after %d durable pages", maxPages)
	return nil, nil, nil
}

func isolatedPursuitLifecyclePostgresDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("HAI_TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping isolated PostgreSQL lifecycle test")
	}
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS")), "true") {
		t.Skip("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS=true is required to create and drop an isolated test database")
	}

	adminConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse PostgreSQL test DSN: %v", err)
	}
	if !isPursuitLifecycleLoopbackHost(adminConfig.Host) {
		t.Fatalf("refusing PostgreSQL lifecycle test against non-loopback host %q", adminConfig.Host)
	}
	if !strings.HasSuffix(strings.ToLower(strings.TrimSpace(adminConfig.Database)), "_test") {
		t.Fatalf("refusing PostgreSQL lifecycle test: HAI_TEST_DATABASE_DSN database %q must end in _test", adminConfig.Database)
	}
	adminConfig.Database = "postgres"
	adminConfig.ConnectTimeout = 5 * time.Second
	adminSQL := stdlib.OpenDB(*adminConfig)
	pingContext, cancelPing := context.WithTimeout(context.Background(), 8*time.Second)
	if err := adminSQL.PingContext(pingContext); err != nil {
		cancelPing()
		_ = adminSQL.Close()
		t.Fatalf("connect to loopback PostgreSQL test server: %v", err)
	}
	cancelPing()

	databaseName := "hai_spw_" + strings.ReplaceAll(uuid.NewString(), "-", "") + "_test"
	quotedDatabase := `"` + databaseName + `"`
	if _, err := adminSQL.ExecContext(context.Background(), "CREATE DATABASE "+quotedDatabase); err != nil {
		_ = adminSQL.Close()
		t.Fatalf("create unique PostgreSQL lifecycle test database: %v", err)
	}

	testConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		_, _ = adminSQL.ExecContext(context.Background(), "DROP DATABASE "+quotedDatabase+" WITH (FORCE)")
		_ = adminSQL.Close()
		t.Fatalf("parse isolated PostgreSQL lifecycle database DSN: %v", err)
	}
	testConfig.Database = databaseName
	testConfig.ConnectTimeout = 5 * time.Second
	testSQL := stdlib.OpenDB(*testConfig)
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: testSQL}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		_ = testSQL.Close()
		_, _ = adminSQL.ExecContext(context.Background(), "DROP DATABASE "+quotedDatabase+" WITH (FORCE)")
		_ = adminSQL.Close()
		t.Fatalf("open isolated PostgreSQL lifecycle database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		_ = testSQL.Close()
		_, _ = adminSQL.ExecContext(context.Background(), "DROP DATABASE "+quotedDatabase+" WITH (FORCE)")
		_ = adminSQL.Close()
		t.Fatalf("get isolated PostgreSQL lifecycle connection pool: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close isolated PostgreSQL lifecycle database: %v", err)
		}
		if _, err := adminSQL.ExecContext(context.Background(), "DROP DATABASE "+quotedDatabase+" WITH (FORCE)"); err != nil {
			t.Errorf("drop isolated PostgreSQL lifecycle database: %v", err)
		}
		if err := adminSQL.Close(); err != nil {
			t.Errorf("close PostgreSQL administration connection: %v", err)
		}
	})

	// These settings keep the fixture on the authoritative versioned migrations
	// and independent of optional vector services configured in a developer shell.
	t.Setenv("DB_AUTOMIGRATE", "false")
	t.Setenv("HAI_SEMANTIC_RETRIEVAL_ENABLED", "false")
	if err := infra.RunMigrations(db); err != nil {
		t.Fatalf("apply versioned migrations to isolated lifecycle test database: %v", err)
	}

	return db
}

func isPursuitLifecycleLoopbackHost(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func countOwnerRows(t *testing.T, db *gorm.DB, model any, owner string) int64 {
	t.Helper()
	var count int64
	if err := db.Model(model).Where("owner_identity = ?", owner).Count(&count).Error; err != nil {
		t.Fatalf("count %T owner rows: %v", model, err)
	}
	return count
}

func countSourceRows(t *testing.T, db *gorm.DB, model any, sourceID uuid.UUID) int64 {
	t.Helper()
	var count int64
	if err := db.Model(model).Where("source_id = ?", sourceID).Count(&count).Error; err != nil {
		t.Fatalf("count %T source rows: %v", model, err)
	}
	return count
}

func mapPursuitLinks(links []models.PursuitLink) map[uuid.UUID]models.PursuitLink {
	result := make(map[uuid.UUID]models.PursuitLink, len(links))
	for _, link := range links {
		result[link.ID] = link
	}
	return result
}
