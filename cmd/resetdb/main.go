package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/intexa/arca-api/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL not set")
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	drops := []string{
		"DROP TABLE IF EXISTS activity_logs CASCADE",
		"DROP TABLE IF EXISTS bank_balance CASCADE",
		"DROP TABLE IF EXISTS settings CASCADE",
		"DROP TABLE IF EXISTS budget_lines CASCADE",
		"DROP TABLE IF EXISTS purchases CASCADE",
		"DROP TABLE IF EXISTS invoices CASCADE",
		"DROP TABLE IF EXISTS transactions CASCADE",
		"DROP TABLE IF EXISTS categories CASCADE",
		"DROP TABLE IF EXISTS siigo_configs CASCADE",
		"DROP TABLE IF EXISTS allowed_domains CASCADE",
		"DROP TABLE IF EXISTS users CASCADE",
		"DROP TABLE IF EXISTS schema_migrations CASCADE",
	}

	for _, sql := range drops {
		if _, err := pool.Exec(context.Background(), sql); err != nil {
			log.Fatalf("drop: %v", err)
		}
	}
	fmt.Println("all tables dropped")

	if err := db.RunMigrations(pool); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	fmt.Println("migrations applied — DB is ready")
}
