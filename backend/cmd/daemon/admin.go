package main

import (
	"context"
	"flag"
	"fmt"
	"strconv"
	"time"

	"daemon/internal/config"
	"daemon/internal/database"
	"daemon/internal/service"
)

// runAdmin — административная CLI (slice 6, RBAC):
//
//	daemon admin keys create --name=<name> [--role=admin|operator|viewer] [--user=<username>] [--expires=<720h>]
//	daemon admin keys list
//	daemon admin keys revoke <id>
func runAdmin(args []string) error {
	if len(args) < 2 || args[0] != "keys" {
		return fmt.Errorf("usage: daemon admin keys create|list|revoke [id]\n  create flags: --name (required), --role (default operator), --user, --expires (e.g. 720h)")
	}
	cmd := args[1]

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	db, err := database.Open(cfg.DBDSN)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		return err
	}
	if cfg.Migrate {
		if err := database.Migrate(context.Background(), db, cfg.Dialect()); err != nil {
			return err
		}
	}
	auth := service.NewAuthService(db, cfg.APIKeys)
	ctx := context.Background()

	switch cmd {
	case "create":
		fs := flag.NewFlagSet("keys create", flag.ContinueOnError)
		name := fs.String("name", "", "имя ключа (required)")
		role := fs.String("role", "operator", "роль: admin | operator | viewer")
		user := fs.String("user", "", "username (default = name)")
		expires := fs.Duration("expires", 0, "срок действия (0 = бессрочно), e.g. 720h")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if *name == "" {
			return fmt.Errorf("--name is required")
		}
		key, plaintext, err := auth.CreateKey(ctx, *name, *role, *user, *expires)
		if err != nil {
			return err
		}
		fmt.Printf("api key created (id=%d, user=%s, role=%s)\n", key.ID, key.UserName, key.RoleName)
		fmt.Printf("key: %s\n", plaintext)
		fmt.Println("save it now — it is shown only once (sha256 hash is stored)")
		return nil

	case "list":
		keys, err := auth.ListKeys(ctx)
		if err != nil {
			return err
		}
		if len(keys) == 0 {
			fmt.Println("no api keys")
			return nil
		}
		fmt.Printf("%-4s %-24s %-16s %-10s %-24s %s\n", "ID", "NAME", "USER", "ROLE", "EXPIRES", "REVOKED")
		for _, k := range keys {
			exp := ""
			if k.ExpiresAt != nil {
				exp = k.ExpiresAt.UTC().Format(time.RFC3339)
			}
			fmt.Printf("%-4d %-24s %-16s %-10s %-24s %v\n", k.ID, k.Name, k.UserName, k.RoleName, exp, k.Revoked)
		}
		return nil

	case "revoke":
		if len(args) < 3 {
			return fmt.Errorf("usage: daemon admin keys revoke <id>")
		}
		id, err := strconv.ParseInt(args[2], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid key id: %s", args[2])
		}
		if err := auth.RevokeKey(ctx, id); err != nil {
			return err
		}
		fmt.Printf("api key %d revoked\n", id)
		return nil

	default:
		return fmt.Errorf("unknown command: %s", cmd)
	}
}
