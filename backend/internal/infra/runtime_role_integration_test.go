//go:build integration

package infra

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestRuntimeRoleCanUseApplicationDataButCannotOwnSchema(t *testing.T) {
	db := integrationDB(t)
	if err := RunMigrations(db); err != nil {
		t.Fatalf("run migrations before runtime-role test: %v", err)
	}

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	role := "hai_runtime_test_" + suffix
	parentRole := "hai_runtime_parent_" + suffix
	password := "runtime-test-only-" + suffix
	fixture := "runtime_role_fixture_" + suffix
	forbidden := "runtime_role_forbidden_" + suffix
	var databaseName string
	if err := db.Raw("SELECT current_database()").Scan(&databaseName).Error; err != nil {
		t.Fatalf("resolve test database: %v", err)
	}

	quotedRole := quotePostgresIdentifier(role)
	quotedParentRole := quotePostgresIdentifier(parentRole)
	quotedFixture := quotePostgresIdentifier(fixture)
	quotedForbidden := quotePostgresIdentifier(forbidden)
	if err := db.Exec("CREATE TABLE public." + quotedFixture + " (id integer PRIMARY KEY, value text NOT NULL)").Error; err != nil {
		t.Fatalf("create existing runtime fixture: %v", err)
	}
	if err := ProvisionRuntimeRole(db, databaseName, role, password); err != nil {
		t.Fatalf("provision runtime role: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Exec("DROP TABLE IF EXISTS public." + quotedForbidden).Error
		_ = db.Exec("DROP TABLE IF EXISTS public." + quotedFixture).Error
		_ = db.Exec("DROP OWNED BY " + quotedRole).Error
		if err := db.Exec("DROP ROLE IF EXISTS " + quotedRole).Error; err != nil {
			t.Errorf("drop runtime test role: %v", err)
		}
		if err := db.Exec("DROP ROLE IF EXISTS " + quotedParentRole).Error; err != nil {
			t.Errorf("drop runtime test parent role: %v", err)
		}
	})
	if err := db.Exec("CREATE ROLE " + quotedParentRole + " NOLOGIN").Error; err != nil {
		t.Fatalf("create inherited parent role: %v", err)
	}
	if err := db.Exec("GRANT " + quotedParentRole + " TO " + quotedRole).Error; err != nil {
		t.Fatalf("grant inherited parent role: %v", err)
	}
	if err := ProvisionRuntimeRole(db, databaseName, role, password); err != nil {
		t.Fatalf("re-provision runtime role with inherited membership: %v", err)
	}

	var flags struct {
		Superuser   bool
		CreateDB    bool
		CreateRole  bool
		Replication bool
		BypassRLS   bool
		Inherit     bool
	}
	if err := db.Raw(`
		SELECT rolsuper AS superuser,
		       rolcreatedb AS create_db,
		       rolcreaterole AS create_role,
		       rolreplication AS replication,
		       rolbypassrls AS bypass_rls,
		       rolinherit AS inherit
		FROM pg_roles WHERE rolname = ?`, role).Scan(&flags).Error; err != nil {
		t.Fatalf("inspect runtime role flags: %v", err)
	}
	if flags.Superuser || flags.CreateDB || flags.CreateRole || flags.Replication || flags.BypassRLS || flags.Inherit {
		t.Fatalf("runtime role retained elevated flags: %+v", flags)
	}
	var inheritedMemberships int64
	if err := db.Raw(`
		SELECT count(*)
		FROM pg_auth_members membership
		JOIN pg_roles member ON member.oid = membership.member
		WHERE member.rolname = ?`, role).Scan(&inheritedMemberships).Error; err != nil {
		t.Fatalf("inspect inherited runtime memberships: %v", err)
	}
	if inheritedMemberships != 0 {
		t.Fatalf("runtime role retained %d inherited membership(s)", inheritedMemberships)
	}

	if err := asDatabaseRole(db, quotedRole, func(tx *gorm.DB) error {
		if err := tx.Exec("INSERT INTO public." + quotedFixture + " (id, value) VALUES (1, 'created')").Error; err != nil {
			return err
		}
		if err := tx.Exec("UPDATE public." + quotedFixture + " SET value = 'updated' WHERE id = 1").Error; err != nil {
			return err
		}
		var value string
		if err := tx.Raw("SELECT value FROM public." + quotedFixture + " WHERE id = 1").Scan(&value).Error; err != nil {
			return err
		}
		if value != "updated" {
			t.Fatalf("runtime read value = %q, want updated", value)
		}
		return tx.Exec("DELETE FROM public." + quotedFixture + " WHERE id = 1").Error
	}); err != nil {
		t.Fatalf("runtime DML boundary rejected application work: %v", err)
	}

	assertDatabaseRoleDenied(t, db, quotedRole, "TRUNCATE public."+quotedFixture)
	assertDatabaseRoleDenied(t, db, quotedRole, "CREATE TABLE public."+quotedForbidden+" (id integer)")
	assertDatabaseRoleDenied(t, db, quotedRole, "CREATE TEMP TABLE "+quotedForbidden+" (id integer)")
	assertDatabaseRoleDenied(t, db, quotedRole, "SELECT version FROM public.schema_migrations LIMIT 1")
}

func asDatabaseRole(db *gorm.DB, quotedRole string, operation func(*gorm.DB) error) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL ROLE " + quotedRole).Error; err != nil {
			return err
		}
		return operation(tx)
	})
}

func assertDatabaseRoleDenied(t *testing.T, db *gorm.DB, quotedRole, statement string) {
	t.Helper()
	if err := asDatabaseRole(db, quotedRole, func(tx *gorm.DB) error {
		return tx.Exec(statement).Error
	}); err == nil {
		t.Fatalf("runtime role was allowed to execute %q", statement)
	}
}
