package infra

import (
	"fmt"
	"os"
	"strings"

	"gorm.io/gorm"
)

const (
	runtimeDBUserEnv     = "DB_RUNTIME_USER"
	runtimeDBPasswordEnv = "DB_RUNTIME_PASSWORD"
)

// ProvisionConfiguredRuntimeRole is called only by `migrate up`. Empty
// runtime-role settings retain backward compatibility for non-Compose users;
// a partially configured role fails closed.
func ProvisionConfiguredRuntimeRole(db *gorm.DB, databaseName string) error {
	user := strings.TrimSpace(os.Getenv(runtimeDBUserEnv))
	password := os.Getenv(runtimeDBPasswordEnv)
	if user == "" && password == "" {
		return nil
	}
	if user == "" || password == "" {
		return fmt.Errorf("%s and %s must both be configured", runtimeDBUserEnv, runtimeDBPasswordEnv)
	}
	if len(password) < 32 || strings.HasPrefix(strings.ToLower(strings.TrimSpace(password)), "change-this-") {
		return fmt.Errorf("%s must be a generated secret of at least 32 characters", runtimeDBPasswordEnv)
	}
	return ProvisionRuntimeRole(db, databaseName, user, password)
}

// ProvisionRuntimeRole creates or rotates a login role and replaces its
// privileges with the minimum HAI backend runtime set. It deliberately does
// not grant schema mutation, migration-ledger, TRUNCATE, database creation,
// role administration, replication, or row-security bypass privileges.
func ProvisionRuntimeRole(db *gorm.DB, databaseName, role, password string) error {
	if db == nil {
		return fmt.Errorf("provision runtime database role: database is required")
	}
	if !safePostgresIdentifier(databaseName) || !safePostgresIdentifier(role) {
		return fmt.Errorf("provision runtime database role: database and role must be safe PostgreSQL identifiers")
	}
	if password == "" {
		return fmt.Errorf("provision runtime database role: password is required")
	}
	var currentUser string
	if err := db.Raw("SELECT current_user").Scan(&currentUser).Error; err != nil {
		return fmt.Errorf("resolve migration owner: %w", err)
	}
	if strings.EqualFold(strings.TrimSpace(currentUser), role) {
		return fmt.Errorf("runtime database role must differ from migration owner %q", currentUser)
	}
	if err := rejectRuntimeObjectOwnership(db, databaseName, role); err != nil {
		return err
	}

	quotedRole := quotePostgresIdentifier(role)
	quotedDatabase := quotePostgresIdentifier(databaseName)
	quotedOwner := quotePostgresIdentifier(currentUser)
	quotedPassword := quotePostgresLiteral(password)

	return db.Transaction(func(tx *gorm.DB) error {
		var exists bool
		if err := tx.Raw("SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = ?)", role).Scan(&exists).Error; err != nil {
			return fmt.Errorf("inspect runtime database role: %w", err)
		}
		if !exists {
			if err := tx.Exec("CREATE ROLE " + quotedRole + " LOGIN PASSWORD " + quotedPassword + " NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS").Error; err != nil {
				return fmt.Errorf("create runtime database role: %w", err)
			}
		} else if err := tx.Exec("ALTER ROLE " + quotedRole + " WITH LOGIN PASSWORD " + quotedPassword + " NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS").Error; err != nil {
			return fmt.Errorf("constrain runtime database role: %w", err)
		}
		if err := revokeRuntimeRoleMemberships(tx, role, quotedRole); err != nil {
			return err
		}

		statements := []string{
			"REVOKE CREATE, TEMPORARY ON DATABASE " + quotedDatabase + " FROM PUBLIC",
			"REVOKE ALL PRIVILEGES ON DATABASE " + quotedDatabase + " FROM " + quotedRole,
			"GRANT CONNECT ON DATABASE " + quotedDatabase + " TO " + quotedRole,
			"REVOKE CREATE ON SCHEMA public FROM PUBLIC",
			"REVOKE ALL PRIVILEGES ON SCHEMA public FROM " + quotedRole,
			"GRANT USAGE ON SCHEMA public TO " + quotedRole,
			"REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM " + quotedRole,
			"GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO " + quotedRole,
			"REVOKE ALL PRIVILEGES ON TABLE public.schema_migrations FROM " + quotedRole,
			"REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public FROM " + quotedRole,
			"GRANT USAGE, SELECT, UPDATE ON ALL SEQUENCES IN SCHEMA public TO " + quotedRole,
			"REVOKE ALL PRIVILEGES ON ALL FUNCTIONS IN SCHEMA public FROM " + quotedRole,
			"GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO " + quotedRole,
			"ALTER DEFAULT PRIVILEGES FOR ROLE " + quotedOwner + " IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO " + quotedRole,
			"ALTER DEFAULT PRIVILEGES FOR ROLE " + quotedOwner + " IN SCHEMA public GRANT USAGE, SELECT, UPDATE ON SEQUENCES TO " + quotedRole,
			"ALTER DEFAULT PRIVILEGES FOR ROLE " + quotedOwner + " IN SCHEMA public GRANT EXECUTE ON FUNCTIONS TO " + quotedRole,
			"ALTER ROLE " + quotedRole + " RESET ALL",
			"ALTER ROLE " + quotedRole + " SET search_path = public",
		}
		for _, statement := range statements {
			if err := tx.Exec(statement).Error; err != nil {
				return fmt.Errorf("apply runtime database privilege boundary: %w", err)
			}
		}
		return nil
	})
}

func rejectRuntimeObjectOwnership(db *gorm.DB, databaseName, role string) error {
	var owned int64
	if err := db.Raw(`
		SELECT
			(SELECT count(*) FROM pg_database d JOIN pg_roles r ON r.oid = d.datdba WHERE r.rolname = ?)
			+ (SELECT count(*) FROM pg_namespace n JOIN pg_roles r ON r.oid = n.nspowner WHERE r.rolname = ?)
			+ (SELECT count(*) FROM pg_class c JOIN pg_roles r ON r.oid = c.relowner WHERE r.rolname = ?)
			+ (SELECT count(*) FROM pg_proc p JOIN pg_roles r ON r.oid = p.proowner WHERE r.rolname = ?)
			+ (SELECT count(*) FROM pg_type t JOIN pg_roles r ON r.oid = t.typowner WHERE r.rolname = ?)
			+ (SELECT count(*) FROM pg_default_acl a JOIN pg_roles r ON r.oid = a.defaclrole WHERE r.rolname = ?)`,
		role, role, role, role, role, role,
	).Scan(&owned).Error; err != nil {
		return fmt.Errorf("inspect runtime database ownership: %w", err)
	}
	if owned > 0 {
		return fmt.Errorf("runtime database role %q owns %d database object(s); reassign ownership before provisioning %q", role, owned, databaseName)
	}
	return nil
}

func revokeRuntimeRoleMemberships(tx *gorm.DB, role, quotedRole string) error {
	var parents []string
	if err := tx.Raw(`
		SELECT parent.rolname
		FROM pg_auth_members membership
		JOIN pg_roles parent ON parent.oid = membership.roleid
		JOIN pg_roles member ON member.oid = membership.member
		WHERE member.rolname = ?`, role).Scan(&parents).Error; err != nil {
		return fmt.Errorf("inspect runtime database role memberships: %w", err)
	}
	for _, parent := range parents {
		if err := tx.Exec("REVOKE " + quotePostgresIdentifier(parent) + " FROM " + quotedRole).Error; err != nil {
			return fmt.Errorf("revoke runtime database role membership %q: %w", parent, err)
		}
	}
	return nil
}

func safePostgresIdentifier(value string) bool {
	if len(value) == 0 || len(value) > 63 {
		return false
	}
	for index, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_' || index > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func quotePostgresIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func quotePostgresLiteral(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}
